package reconboot

type Bootstrapper interface {
	Install() error
	Uninstall() error
}
