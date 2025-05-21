package main

import (
	"errors"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	"jzz/ip/config"
	"jzz/ip/utils"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

var e = echo.New()

func main() {
	config.InitConfig()
	s := NewGeoIPService()
	s.Init()

	// api
	e.GET("/:ip", QueryIp)
	e.GET("/", QueryIp)

	go func() {
		err := e.Start(fmt.Sprintf(":%d", config.Cfg.Port))
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorf(err.Error())
		}
	}()
	<-signals()
}

func QueryIp(c echo.Context) error {
	ip := c.Param("ip")
	if ip == "" {
		ip = c.RealIP()
	}
	if ip == "" || utils.IsLocalIP(ip) {
		return c.JSON(404, map[string]interface{}{
			"ip": ip,
		})
	}
	address := net.ParseIP(ip)
	city, err := geoDb.City(address)
	if err != nil {
		return c.JSON(500, map[string]interface{}{
			"ip": ip,
		})
	}
	region := ""
	if city.Subdivisions != nil && len(city.Subdivisions) > 0 {
		region = city.Subdivisions[0].Names["zh-CN"]
	}
	return c.JSONPretty(200, map[string]interface{}{
		"ip": ip,
		"city": map[string]interface{}{
			"country":   city.Country.Names["zh-CN"],
			"region":    region,
			"city":      city.City.Names["zh-CN"],
			"continent": city.Continent.Names["zh-CN"],
		},
		"location": map[string]interface{}{
			"latitude":  city.Location.Latitude,
			"longitude": city.Location.Longitude,
			"timezone":  city.Location.TimeZone,
			"accuracy":  city.Location.AccuracyRadius,
			"metroCode": city.Location.MetroCode,
		},
	}, "  ")
}

func signals() <-chan bool {
	quit := make(chan bool)
	s := make(chan os.Signal)
	signal.Notify(s, syscall.SIGQUIT, syscall.SIGTERM, os.Interrupt, syscall.SIGSEGV)
	go func() {
		defer func() {
			close(s)
			close(quit)
			signal.Stop(s)
		}()
		<-s
		destroy()
		_ = e.Shutdown(nil)
	}()
	return quit
}

func destroy() {
	log.Info("services destroy")
	_ = e.Shutdown(nil)
	co.Stop()
}
