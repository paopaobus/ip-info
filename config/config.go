package config

import (
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type Config struct {
	Port int
	Ip   IPConfigure
}
type IPConfigure struct {
	AutoUpdate bool
	Path       string
	DbDownUrl  string
	AccountId  string
	LicenseKey string
}

var Cfg = Config{}
var active = pflag.String("configActive", "config", "config active")
var path = pflag.String("configPath", "config", "config path")

func InitConfig() {
	pflag.Parse()
	viper.SetConfigType("yaml")
	viper.SetConfigName(*active)
	viper.AddConfigPath(*path)
	if err := viper.ReadInConfig(); err != nil {
		panic(err)
	}
	err := viper.Unmarshal(&Cfg)
	if err != nil {
		panic(err)
	}
}
