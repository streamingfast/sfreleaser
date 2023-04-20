package main

import (
	"errors"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/streamingfast/cli"
	"go.uber.org/zap"
)

func ConfigureReleaserConfigFile() cli.CommandOption {
	configurer := func(_ *cobra.Command, _ []string) {
		configIn := "."
		if viper.GetString("global.root") != "" {
			configIn = viper.GetString("global.root")
		}

		zlog.Debug("configuring viper config location", zap.String("config_path", configIn))
		viper.AddConfigPath(configIn)
		viper.SetConfigName(".sfreleaser")
		viper.SetConfigType("yaml")

		if err := viper.ReadInConfig(); err != nil {
			notFoundErr := viper.ConfigFileNotFoundError{}
			if !errors.As(err, &notFoundErr) {
				cli.NoError(err, "Loading config file failed")
			}
		}
	}

	return cli.CommandOptionFunc(func(cmd *cobra.Command) {
		root := cmd.Root()

		hook := configurer
		if actual := root.PersistentPreRun; actual != nil {
			hook = func(cmd *cobra.Command, args []string) {
				configurer(cmd, args)
				actual(cmd, args)
			}
		}

		root.PersistentPreRun = hook
	})
}
