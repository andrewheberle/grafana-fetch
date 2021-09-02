package cmd

import (
	"os"

	"github.com/rs/zerolog/log"
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
	viper.BindPFlags(rootCmd.PersistentFlags())
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
			log.Fatal().Err(err).Send()
		}

		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName("grafana-fetch")
	}

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Warn().Err(err).Msg("no config file loaded")
		} else {
			log.Fatal().Err(err).Send()
		}
	}

	// watch for config changes
	viper.WatchConfig()
}
