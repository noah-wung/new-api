package reporterupdate

import (
	"os"
	"strconv"
)

// OpenLog bounds background diagnostic files to four 2.5 MiB generations.
// Callers have one writer per task; rotation never touches usage data.
func OpenLog(path string) (*os.File, error) {
	if info, e := os.Stat(path); e == nil && info.Size() >= 2560<<10 {
		_ = os.Remove(path + ".3")
		for n := 2; n >= 1; n-- {
			_ = os.Rename(path+"."+strconv.Itoa(n), path+"."+strconv.Itoa(n+1))
		}
		if e = os.Rename(path, path+".1"); e != nil {
			return nil, e
		}
	} else if e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}
