package bootstrapper

type Bootstrapper interface {
	Install() error
	Uninstall() error
}
