package dynamiclistener

import (
	"crypto/tls"
	"net"
	"testing"
	"time"

	"github.com/rancher/dynamiclistener/factory"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	apiError "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func Test_getCertificate(t *testing.T) {
	beforeKey, beforeCert, err := newCertificate()
	assert.NoError(t, err, "Error when setting up test - unable to construct before key for test")
	beforeTLSCert, err := tls.X509KeyPair(beforeCert, beforeKey)
	assert.NoError(t, err, "Error when setting up test - unable to convert before to tls.Certificate")
	afterKey, afterCert, err := newCertificate()
	assert.NoError(t, err, "Error when setting up test - unable to construct after key for test")
	afterTLSCert, err := tls.X509KeyPair(afterCert, afterKey)
	assert.NoError(t, err, "Error when setting up test - unable to convert after to tls.Certificate")
	tests := []struct {
		// input test vars
		name          string
		secret        *v1.Secret
		secretErr     error
		cachedCert    *tls.Certificate
		cachedVersion string
		currentConn   *closeWrapper
		otherConns    map[int]*closeWrapper

		// output/result test vars
		closedConns  []int
		expectedCert *tls.Certificate
		wantError    bool
	}{
		{
			name:   "no secret found",
			secret: nil,
			secretErr: apiError.NewNotFound(schema.GroupResource{
				Group:    "",
				Resource: "Secret",
			}, "testSecret"),
			currentConn: &closeWrapper{id: 0},
			otherConns:  map[int]*closeWrapper{},

			expectedCert: nil,
			wantError:    true,
		},
		{
			name: "secret found, and is up to date",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					ResourceVersion: "1",
					Name:            "testSecret",
					Namespace:       "test",
				},
				Data: map[string][]byte{
					v1.TLSCertKey:       beforeCert,
					v1.TLSPrivateKeyKey: beforeKey,
				},
			},
			cachedVersion: "1",
			cachedCert:    &beforeTLSCert,
			currentConn:   &closeWrapper{id: 0},
			otherConns:    map[int]*closeWrapper{},

			expectedCert: &beforeTLSCert,
			wantError:    false,
		},
		{
			name: "secret found, is not up to date, but k8s secret is not valid",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					ResourceVersion: "2",
					Name:            "testSecret",
					Namespace:       "test",
				},
				Data: map[string][]byte{
					v1.TLSPrivateKeyKey: []byte("strawberry"),
				},
			},
			cachedVersion: "1",
			cachedCert:    &beforeTLSCert,
			currentConn:   &closeWrapper{id: 0},
			otherConns:    map[int]*closeWrapper{},

			expectedCert: &beforeTLSCert,
			wantError:    false,
		},
		{
			name: "secret found, but is not up to date",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					ResourceVersion: "2",
					Name:            "testSecret",
					Namespace:       "test",
				},
				Data: map[string][]byte{
					v1.TLSCertKey:       afterCert,
					v1.TLSPrivateKeyKey: afterKey,
				},
			},
			cachedVersion: "1",
			cachedCert:    &beforeTLSCert,
			currentConn:   &closeWrapper{id: 0},
			otherConns:    map[int]*closeWrapper{},

			expectedCert: &afterTLSCert,
			wantError:    false,
		},
		{
			name: "secret found, is not up to date, and we have conns using current cert",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					ResourceVersion: "2",
					Name:            "testSecret",
					Namespace:       "test",
				},
				Data: map[string][]byte{
					v1.TLSCertKey:       afterCert,
					v1.TLSPrivateKeyKey: afterKey,
				},
			},
			cachedVersion: "1",
			cachedCert:    &beforeTLSCert,
			currentConn:   &closeWrapper{id: 0},
			otherConns: map[int]*closeWrapper{
				1: {
					id:    1,
					ready: false,
					Conn:  &fakeConn{},
				},
				2: {
					id:    2,
					ready: true,
					Conn:  &fakeConn{},
				},
			},

			closedConns:  []int{2},
			expectedCert: &afterTLSCert,
			wantError:    false,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			testConns := test.otherConns
			if testConns != nil {
				testConns[test.currentConn.id] = test.currentConn
				// make sure our conn is listed as one of the current connections
			}
			l := listener{
				cert:    test.cachedCert,
				version: test.cachedVersion,
				storage: &MockTLSStorage{
					Secret:    test.secret,
					SecretErr: test.secretErr,
				},
				conns: testConns,
			}
			for _, conn := range testConns {
				conn.l = &l
			}
			newCert, err := l.getCertificate(&tls.ClientHelloInfo{Conn: test.currentConn})
			if test.wantError {
				assert.Errorf(t, err, "expected an error but none was provdied")
			} else {
				assert.NoError(t, err, "did not expect an error but got one")
			}
			assert.Equal(t, test.expectedCert, newCert, "expected cert did not match actual cert")
			if test.expectedCert != nil && test.wantError == false && test.currentConn != nil && test.otherConns != nil {
				assert.True(t, test.currentConn.ready, "expected connection to be ready but it was not")
			} else {
				if test.currentConn != nil {
					assert.False(t, test.currentConn.ready, "did not expect connection to be ready")
				}
			}
			for _, closedConn := range test.closedConns {
				_, ok := l.conns[closedConn]
				assert.False(t, ok, "closed conns should not be found")
			}
		})
	}
}

func newCertificate() ([]byte, []byte, error) {
	cert, key, err := factory.GenCA()
	if err != nil {
		return nil, nil, err
	}

	return factory.MarshalChain(key, cert)
}

type MockTLSStorage struct {
	Secret    *v1.Secret
	SecretErr error
}

func (m *MockTLSStorage) Get() (*v1.Secret, error) {
	return m.Secret, m.SecretErr
}

func (m *MockTLSStorage) Update(secret *v1.Secret) error {
	panic("Not implemented")
}

// adapted from k8s.io/apimachinery@v0.18.8/pkg/util.proxy/ugradeaware_test.go
type fakeConn struct{}

func (f *fakeConn) Read([]byte) (int, error)        { return 0, nil }
func (f *fakeConn) Write([]byte) (int, error)       { return 0, nil }
func (f *fakeConn) Close() error                    { return nil }
func (fakeConn) LocalAddr() net.Addr                { return nil }
func (fakeConn) RemoteAddr() net.Addr               { return nil }
func (fakeConn) SetDeadline(t time.Time) error      { return nil }
func (fakeConn) SetReadDeadline(t time.Time) error  { return nil }
func (fakeConn) SetWriteDeadline(t time.Time) error { return nil }
