package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/base64"
	"github.com/google/uuid"
	"github.com/labstack/gommon/log"
	"github.com/robfig/cron/v3"
	"io"
	"jzz/ip/config"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/geoip2-golang"
)

const (
	filename             = "GeoLite2-City.mmdb"
	downloadTempFilename = "GeoLite2-City.tar.gz"
)

var geoDb *geoip2.Reader

var updateMutex sync.Mutex

var co = cron.New(cron.WithSeconds(), cron.WithChain(cron.SkipIfStillRunning(cron.DefaultLogger)))

type GeoIPService struct {
}

func NewGeoIPService() *GeoIPService {
	service := &GeoIPService{}
	service.reloadDb(true)
	if config.Cfg.Ip.AutoUpdate {
		go func() {
			// 使用 Cron 表达式设置任务在每周的周一 10 点执行
			_, _ = co.AddFunc("0 0 10 * * 1", func() {
				service.AutoUpdate()
			})
			co.Start()
		}()
	}
	return service
}

func (s *GeoIPService) AutoUpdate() {
	if !config.Cfg.Ip.AutoUpdate {
		return
	}
	filePath := filepath.Join(config.Cfg.Ip.Path, filename)
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		log.Error("数据库文件不存在:", err)
		return
	}

	date := s.headDb(config.Cfg.Ip.DbDownUrl)
	if date.IsZero() {
		return
	}

	// 检测本地文件是否过期
	localLastModified := fileInfo.ModTime().UnixMilli()
	lastModified := date.UnixMilli()

	if localLastModified < lastModified {
		if s.updateDb() {
			s.reloadDb(false)
		}
	}
}

func (s *GeoIPService) updateDb() bool {
	log.Infof("开始更新数据库 geoIp")

	if !s.downloadDb(config.Cfg.Ip.DbDownUrl) {
		return false
	}
	log.Infof("geo ip数据库下载成功")

	// 文件进行解压
	uid := uuid.New()
	tarFile := filepath.Join(config.Cfg.Ip.Path, downloadTempFilename)
	destDir := filepath.Join(config.Cfg.Ip.Path, "temp-"+uid.String())

	if !s.unTar(tarFile, destDir) {
		return false
	}
	log.Infof("geo ip数据库解压成功")

	// 检测文件夹是否存在 GeoLite2-
	files, err := os.ReadDir(destDir)
	if err != nil {
		log.Error("读取解压目录失败:", err)
		return false
	}
	mmdbPath := ""
	for _, file := range files {
		if file.IsDir() && strings.HasPrefix(file.Name(), "GeoLite2-") {
			mmdbPath = filepath.Join(destDir, file.Name(), filename)
			break
		}
	}
	if _, err := os.Stat(mmdbPath); os.IsNotExist(err) {
		log.Error("geo ip数据库未识别到文件")
		return false
	}
	// 移动文件
	targetPath := filepath.Join(config.Cfg.Ip.Path, filename)
	if err := os.Rename(mmdbPath, targetPath); err != nil {
		log.Error("移动文件失败:", err)
		return false
	}
	// 清理临时文件
	_ = os.RemoveAll(destDir)
	_ = os.Remove(tarFile)
	log.Infof("geo ip数据库文件更新成功")
	return true
}

func (s *GeoIPService) reloadDb(retry bool) bool {
	dbPath := filepath.Join(config.Cfg.Ip.Path, filename)
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		if os.IsNotExist(err) {
			err1 := os.MkdirAll(config.Cfg.Ip.Path, 0755)
			if err1 != nil {
				log.Error("创建数据库目录失败:", err1.Error())
				return false
			}
		}
		if config.Cfg.Ip.AutoUpdate && retry {
			if !s.updateDb() {
				return s.reloadDb(false)
			}
		}
		log.Error("ip数据库文件不存在 加载失败")
		return false
	}
	// 关闭旧的reader
	if geoDb != nil {
		_ = geoDb.Close()
	}
	var err error
	geoDb, err = geoip2.Open(dbPath)
	if err != nil {
		log.Error("ip数据库加载失败:", err)
		return false
	}
	return true
}

func (s *GeoIPService) headDb(urlStr string) time.Time {
	client := &http.Client{}
	req, err := http.NewRequest("HEAD", urlStr, nil)
	if err != nil {
		log.Error("创建HEAD请求失败:", err)
		return time.Time{}
	}
	req.SetBasicAuth(config.Cfg.Ip.AccountId, config.Cfg.Ip.LicenseKey)
	resp, err := client.Do(req)
	if err != nil {
		log.Error("查询最后更新时间错误:", err)
		return time.Time{}
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	lastModified := resp.Header.Get("Last-Modified")
	if lastModified == "" {
		return time.Time{}
	}
	// 解析HTTP日期格式
	t, err := time.Parse(time.RFC1123, lastModified)
	if err != nil {
		log.Error("解析Last-Modified时间失败:", err)
		return time.Time{}
	}
	return t
}

func (s *GeoIPService) downloadDb(urlStr string) bool {
	updateMutex.Lock()
	defer updateMutex.Unlock()
	client := &http.Client{}
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		log.Error("创建下载请求失败:", err)
		return false
	}
	auth := base64.StdEncoding.EncodeToString([]byte(config.Cfg.Ip.AccountId + ":" + config.Cfg.Ip.LicenseKey))
	req.Header.Add("Authorization", "Basic "+auth)

	resp, err := client.Do(req)
	if err != nil {
		log.Error("下载ip数据库失败:", err)
		return false
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		log.Error("下载ip数据库失败, 状态码:", resp.StatusCode)
		return false
	}
	tarFile := filepath.Join(config.Cfg.Ip.Path, downloadTempFilename)
	out, err := os.Create(tarFile)
	if err != nil {
		log.Error("创建下载文件失败:", err)
		return false
	}
	defer func() {
		_ = out.Close()
	}()
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		log.Error("写入下载文件失败:", err)
		return false
	}
	return true
}

func (s *GeoIPService) unTar(tarFile, destDir string) bool {
	updateMutex.Lock()
	defer updateMutex.Unlock()
	// 确保目标目录存在
	if err := os.MkdirAll(destDir, 0755); err != nil {
		log.Error("创建解压目录失败:", err)
		return false
	}
	file, err := os.Open(tarFile)
	if err != nil {
		log.Error("打开tar文件失败:", err)
		return false
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		log.Error("创建gzip reader失败:", err)
		return false
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Info("读取tar文件失败:", err)
			return false
		}
		// 获取文件信息
		target := filepath.Join(destDir, header.Name)
		// 检查文件类型
		switch header.Typeflag {
		case tar.TypeDir:
			// 创建目录
			if err := os.MkdirAll(target, 0755); err != nil {
				log.Error("创建目录失败:", err)
				return false
			}
		case tar.TypeReg:
			// 创建文件
			dir := filepath.Dir(target)
			if err := os.MkdirAll(dir, 0755); err != nil {
				log.Error("创建父目录失败:", err)
				return false
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				log.Error("创建文件失败:", err)
				return false
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				log.Error("写入文件失败:", err)
				return false
			}
			f.Close()
		}
	}
	return true
}
