package quiet

import "os"

// SilenceStdout runs f with os.Stdout redirected to /dev/null, restoring it afterward.
func SilenceStdout(f func() error) error {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}
	defer devNull.Close()
	old := os.Stdout
	os.Stdout = devNull
	defer func() { os.Stdout = old }()
	return f()
}
