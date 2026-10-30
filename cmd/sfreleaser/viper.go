package main

import (
	"errors"
	"slices"

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

		inheritReleaseConfig()
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

// inheritReleaseConfig makes the 'release' section of the config apply to the release
// subcommands, whose flags are bound to 'release.<subcommand>.<flag>'.
func inheritReleaseConfig() {
	subcommands := []string{"detect", "publish"}

	settings := viper.GetStringMap("release")
	for _, subcommand := range subcommands {
		for key, value := range settings {
			if !slices.Contains(subcommands, key) {
				viper.SetDefault("release."+subcommand+"."+key, value)
			}
		}
	}
}
