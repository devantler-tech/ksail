package validators

type Validator interface {
	Validate() error
}
