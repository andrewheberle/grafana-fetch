package cmd

import (
	"os"

	"log/slog"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	rootCmd = &cobra.Command{
		Use:   "grafana-fetch",
		Short: "A tool to fetch, cache and serve rendered images from Grafana",
	}
)

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// set up flags
	rootCmd.PersistentFlags().String("config", "", "config file (default is $HOME/grafana-fetch.yaml)")
	rootCmd.PersistentFlags().BoolP("insecure", "k", false, "allow insecure SSL connections")
	rootCmd.PersistentFlags().String("cafile", "", "CA file")

	// bind flags to viper
	_ = viper.BindPFlags(rootCmd.PersistentFlags())
}

func initConfig() {
	// Do viper init
	viper.SetEnvPrefix("gf_fetch")
	viper.AutomaticEnv()

	// Load config if provided
	if viper.IsSet("config") {
		viper.SetConfigFile(viper.GetString("config"))
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			slog.Error("could not find users home dir", "error", err)
			os.Exit(1)
		}

		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName("grafana-fetch")
	}

	// load config
	if err := viper.ReadInConfig(); err != nil {
		// error here is always fatal if config was provided
		if viper.IsSet("config") {
			slog.Error("could not load configuration file", "error", err)
			os.Exit(1)
		}

		// otherwise if default config was not found only fail if config was invalid
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			slog.Warn("no config file loaded", "error", err)
		} else {
			slog.Error("could not parse configuration file", "error", err)
			os.Exit(1)
		}
	} else {
		// watch for config changes
		viper.WatchConfig()
	}
}
