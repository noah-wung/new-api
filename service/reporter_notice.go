package service

import (
	"debug/buildinfo"
	"errors"
	"os"
	"path/filepath"
	"regexp"

	"github.com/QuantumNous/new-api/pkg/reporterupdate"
)

var reporterVersionFlag = regexp.MustCompile(`(?:^|\s)-X\s+['"]?github\.com/QuantumNous/new-api/pkg/reporterupdate\.Version=([0-9]+\.[0-9]+\.[0-9]+)['"]?(?:\s|$)`)

// Read the actual downloadable asset, not the gateway's own build version.
// Older/custom binaries without explicit version metadata are not advertised.
func ReporterNoticeVersion(target string) (reporterupdate.NoticeInfo, error) {
	spec, err := usageReporterBinarySpecForTarget(target)
	if err != nil {
		return reporterupdate.NoticeInfo{}, err
	}
	dirs := append(usageReporterBinarySearchDirs(), filepath.Join(os.TempDir(), usageReporterBinaryPrefix))
	for _, dir := range dirs {
		path := filepath.Join(dir, spec.Target, spec.DownloadName)
		if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
			return reporterBinaryNotice(path)
		}
	}
	return reporterupdate.NoticeInfo{}, errors.New("reporter asset unavailable")
}

func reporterBinaryNotice(path string) (reporterupdate.NoticeInfo, error) {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return reporterupdate.NoticeInfo{}, err
	}
	for _, setting := range info.Settings {
		if setting.Key != "-ldflags" {
			continue
		}
		match := reporterVersionFlag.FindStringSubmatch(setting.Value)
		if len(match) == 2 && reporterupdate.ValidVersion(match[1]) {
			return reporterupdate.NoticeInfo{Version: match[1]}, nil
		}
	}
	return reporterupdate.NoticeInfo{}, errors.New("reporter asset version unavailable")
}
