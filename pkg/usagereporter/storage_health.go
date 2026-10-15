package usagereporter

import "os"

// CheckStorage confirms writable durable storage without inventing an event.
func (r *Reporter) CheckStorage() error {
	f, e := os.CreateTemp(r.queueDir, ".health-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write([]byte("storage-check")); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	return f.Close()
}
