package cmd

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	uuid "github.com/satori/go.uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type Dashboard struct {
	Path  string
	Ttl   int
	Token string
	Org   int
	Theme string
}

var (
	serverCmd = &cobra.Command{
		Use:   "server",
		Short: "Serve rendered Grafana images",
		Run: func(cmd *cobra.Command, args []string) {
			runServer()
		},
	}

	errorInvalidOptions = fmt.Errorf("invalid options")
	certPool            *x509.CertPool
)

func init() {
	rootCmd.AddCommand(serverCmd)

	// command line flags
	serverCmd.Flags().String("listen", ":8080", "listen address")
	serverCmd.Flags().String("url", "http://grafana:3000", "grafana base url")
	serverCmd.Flags().String("cache", "", "cache directory")

	// bind flags to viper
	_ = viper.BindPFlag("listen", serverCmd.Flags().Lookup("listen"))
	_ = viper.BindPFlag("url", serverCmd.Flags().Lookup("url"))
	_ = viper.BindPFlag("cache", serverCmd.Flags().Lookup("cache"))
}

func runServer() {
	// validate provided url
	if _, err := url.Parse(viper.GetString("url")); err != nil {
		slog.Error("invalid URL", "error", err)
		os.Exit(1)
	}

	// handle if user provided CA file
	if viper.IsSet("cafile") {
		// attempt to load system cert pool initially
		pool, err := x509.SystemCertPool()
		if err != nil {
			// if this fails start with an empty certpool
			pool = x509.NewCertPool()
		}

		// attempt to load provided ca file
		pem, err := os.ReadFile(viper.GetString("cafile"))
		if err != nil {
			slog.Error("could not load CA file", "error", err)
			os.Exit(1)
		}

		// add pem file to cert pool
		if ok := pool.AppendCertsFromPEM(pem); !ok {
			slog.Warn("problem adding CA file to certificate pool")
		}

		// use resulting certificate pool
		certPool = pool
	} else {
		certPool = nil
	}

	r := httprouter.New()
	r.GET("/:dashboard/:panel/:from/:to", graphHandler)
	r.GET("/:dashboard/:panel/:options/:from/:to", graphHandler)
	r.GET("/", catchAll)

	srv := http.Server{
		Addr:         viper.GetString("listen"),
		Handler:      r,
		ReadTimeout:  time.Second * 30,
		WriteTimeout: time.Second * 30,
	}

	slog.Info("starting server",
		"listen", viper.GetString("listen"),
		"cache", viper.GetString("cache"))
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("error from server", "error", err)
		os.Exit(1)
	}
}

func catchAll(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	id := uuid.NewV4()

	slog.Info("not found",
		"uuid", id.String(),
		"method", r.Method,
		"path", r.URL.Path,
		"query", r.URL.RawQuery,
		"remote", r.RemoteAddr,
		"status", http.StatusNotFound)

	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintf(w, "404 - Not Found")
}

func graphHandler(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	var dashboard Dashboard
	var cacheFile string
	var optString string

	// vars := mux.Vars(r)

	// generate a uuid to track this request
	id := uuid.NewV4()

	logger := slog.With("uuid", id.String()).
		With("method", r.Method).
		With("path", r.URL.Path).
		With("query", r.URL.RawQuery).
		With("remote", r.RemoteAddr)

	// parse provided options
	options, err := parseOptions(ps.ByName("options"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "400 Bad Request")
		logger.Warn("invalid options", "error", err, "options", optString)
		return
	}

	// check dashboard config exists and is correct type
	d := ps.ByName("dashboard")
	dashboards := viper.Sub("dashboards")
	if !dashboards.IsSet(d) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "404 Not Found")
		logger.Error("not found", "dashboard", d, "status", http.StatusNotFound)
		return
	}

	if err := dashboards.UnmarshalKey(d, &dashboard); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "500 Internal Server Error")
		logger.Error("invalid config",
			"error", err,
			"dashboard", d,
			"status", http.StatusInternalServerError)
		return
	}

	// build url
	graphUrl := generateUrl(dashboard, r.URL.RawQuery, options, ps)

	// return cached data if still fresh
	if viper.IsSet("cache") {
		h := sha256.New()
		_, _ = h.Write([]byte(d + graphUrl.RawQuery))

		cacheFile = filepath.Join(viper.GetString("cache"), fmt.Sprintf("%x", h.Sum(nil)))
		ttl := getInt(dashboard.Ttl, "ttl")

		// check if cached file exists and is fresh
		if info, err := os.Stat(cacheFile); err == nil && time.Since(info.ModTime()) < time.Second*time.Duration(ttl) {
			if err := func() error {
				// open cached file
				f, err := os.Open(cacheFile)
				if err != nil {
					return err
				}
				defer f.Close()

				// build cache-control header
				cachecontrol := fmt.Sprintf("public, max-age=%0.0f, immutable", ((time.Second * time.Duration(ttl)) - time.Since(info.ModTime())).Seconds())
				w.Header().Add("Cache-Control", cachecontrol)

				// write cached version back and finish
				w.WriteHeader(http.StatusOK)
				_, _ = io.Copy(w, f)

				return nil
			}(); err == nil {
				// no error from closure means cached response was ok so finish here
				logger.Info("returned cached response",
					"cachefile", cacheFile,
					"age", time.Since(info.ModTime()).Seconds(),
					"ttl", (time.Second * time.Duration(ttl)).Seconds(),
					"status", http.StatusOK)
				return
			} else {
				// an error is logged but not fatal
				logger.Error("error returning cached response",
					"error", err,
					"cachefile", cacheFile)
			}
		} else {
			logger.Error("could not return cached response",
				"error", err,
				"cachefile", cacheFile)
		}
	}

	// create new http transport (to manage SSL settings), client and request
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: viper.GetBool("insecure"),
			RootCAs:            certPool,
		},
	}

	client := &http.Client{
		Transport: tr,
		Timeout:   time.Second * 30,
	}
	req, err := http.NewRequest("GET", graphUrl.String(), nil)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "500 Internal Server Error")
		logger.Error("problem fetching graph",
			"error", err,
			"dashboard", d,
			"url", graphUrl.String(),
			"status", http.StatusInternalServerError)
		return
	}

	// set auth token for request
	if token := getString(dashboard.Token, "token"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	// perform request
	logger.Info("sending request",
		"url", graphUrl.String())
	resp, err := client.Do(req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "500 Internal Server Error")
		logger.Error("problem fetching graph",
			"error", err,
			"dashboard", d,
			"url", graphUrl.String(),
			"status", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	logger.Info("request complete",
		"url", graphUrl.String(),
		"status", resp.StatusCode,
		"content-type", resp.Header.Get("Content-Type"))

	// if request was ok then add cache control header
	if resp.StatusCode == http.StatusOK {
		cachecontrol := fmt.Sprintf("public, max-age=%d, immutable", getInt(dashboard.Ttl, "ttl"))
		w.Header().Add("Cache-Control", cachecontrol)
	}

	// return same status as our request
	w.WriteHeader(resp.StatusCode)
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	}

	if resp.StatusCode == http.StatusOK && cacheFile != "" {
		// attempt to open cache file for writing
		if name, err := func() (string, error) {
			tmp, err := os.CreateTemp(viper.GetString("cache"), ".cache-*")
			if err != nil {
				return "", err
			}
			defer tmp.Close()

			// write response and to cache
			wr := io.MultiWriter(w, tmp)
			_, err = io.Copy(wr, resp.Body)
			if err != nil {
				return tmp.Name(), err
			}

			return tmp.Name(), nil
		}(); err != nil {
			logger.Error("error trying to cache file", "error", err)
			// clean up temp file and nothing else as we had a failure
			if name != "" {
				os.Remove(name)
			}
		} else {
			// attempt to move temp file into place (failure here is not fata)
			if err := os.Rename(name, cacheFile); err != nil {
				logger.Error("error trying to rename temp file", "error", err)
			}
		}
	}

	// last resort is writing response to client only
	_, _ = io.Copy(w, resp.Body)
}

func parseOptions(opts string) (map[string]string, error) {

	options := make(map[string]string)
	if strings.Contains(opts, ",") {
		for _, opt := range strings.Split(opts, ",") {
			kv := strings.Split(opt, "=")
			if len(kv) != 2 {
				return nil, errorInvalidOptions
			}
			options[kv[0]] = kv[1]
		}
	}

	// check values and set defaults
	w, wok := options["width"]
	h, hok := options["height"]

	if !wok && hok {
		options["width"] = h
	} else if !hok && wok {
		options["height"] = w
	} else if !wok && !hok {
		options["width"] = "1000"
		options["height"] = "500"
	}

	if _, ok := options["theme"]; !ok {
		options["theme"] = "light"
	}
	return options, nil
}

func getString(val, key string) string {
	return getStringD(val, key, "")
}

func getStringD(val, key, def string) string {
	if val != "" {
		return val
	}

	if viper.IsSet(key) {
		return viper.GetString(key)
	}

	return def
}

func getInt(val int, key string) int {
	return getIntD(val, key, 0)
}

func getIntD(val int, key string, def int) int {
	if val != 0 {
		return val
	}

	if viper.IsSet(key) {
		return viper.GetInt(key)
	}

	return def
}

func generateUrl(dashboard Dashboard, query string, options map[string]string, ps httprouter.Params) *url.URL {
	// build url to fetch graph
	graphUrl, _ := url.Parse(viper.GetString("url"))

	// build query string
	graphUrl.RawQuery = query
	queryValues := graphUrl.Query()

	// add fixed vars from config
	queryValues.Add("orgId", fmt.Sprintf("%d", getIntD(dashboard.Org, "org", 1)))
	queryValues.Add("theme", getStringD(dashboard.Theme, "theme", "light"))

	// add vars to query string
	queryValues.Add("panelId", ps.ByName("panel"))
	for _, key := range []string{"from", "to"} {
		queryValues.Add(key, ps.ByName(key))
	}

	// add options to query string
	for _, key := range []string{"width", "height"} {
		queryValues.Add(key, options[key])
	}
	graphUrl.RawQuery = queryValues.Encode()
	graphUrl.Path = path.Join(graphUrl.Path, "render", dashboard.Path)

	return graphUrl

}
