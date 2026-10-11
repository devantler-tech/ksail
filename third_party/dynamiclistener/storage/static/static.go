package static

import (
	"github.com/rancher/dynamiclistener/factory"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Storage struct {
	Secret *v1.Secret
}

func New(certPem, keyPem []byte) *Storage {
	return &Storage{
		Secret: &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					factory.Static: "true",
				},
			},
			Data: map[string][]byte{
				v1.TLSCertKey:       certPem,
				v1.TLSPrivateKeyKey: keyPem,
			},
			Type: v1.SecretTypeTLS,
		},
	}
}

func (s *Storage) Get() (*v1.Secret, error) {
	return s.Secret, nil
}

func (s *Storage) Update(_ *v1.Secret) error {
	return nil
}
