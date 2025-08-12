package reconciliationtoolbootstrapper

type Bootstrapper interface {
	Install() error
	Uninstall() error
}
