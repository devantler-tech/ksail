package clusterapi

// NewTestService returns a Service whose provisioner factory is overridden, so black-box tests can
// substitute fake provisioners without touching the real Docker-backed factory.
func NewTestService(factory FactoryFunc) *Service {
	service := NewService()
	service.newFactory = factory

	return service
}
