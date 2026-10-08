package api

import (
	"context"
	"encoding/json"
	"net/http"
)

// ClusterAuthentication reports whether the selected context needs a supported local sign-in.
type ClusterAuthentication struct {
	Enabled   bool   `json:"enabled"`
	Supported bool   `json:"supported"`
	Required  bool   `json:"required"`
	Message   string `json:"message,omitempty"`
}

// ClusterAuthenticationService supplies explicit sign-in for local kubeconfig contexts.
type ClusterAuthenticationService interface {
	Authentication(ctx context.Context, namespace, name string) (ClusterAuthentication, error)
	RenewAuthentication(ctx context.Context, namespace, name string) error
}

func (s *Server) authenticationEnabled() bool {
	return s.Mode == ModeLocal && !s.ReadOnly &&
		s.AWSSSORenewalEnabled != nil && s.AWSSSORenewalEnabled()
}

func (s *Server) handleClusterAuthentication(writer http.ResponseWriter, request *http.Request) {
	if !s.authenticationEnabled() {
		writeJSON(writer, http.StatusOK, ClusterAuthentication{
			Message: "AWS SSO renewal is disabled. Enable it in Settings > Credentials.",
		})

		return
	}

	service, ok := s.Service.(ClusterAuthenticationService)
	if !ok {
		http.NotFound(writer, request)

		return
	}

	info, err := service.Authentication(
		request.Context(),
		request.PathValue("namespace"),
		request.PathValue("name"),
	)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	info.Enabled = true
	writeJSON(writer, http.StatusOK, info)
}

func (s *Server) handleRenewClusterAuthentication(
	writer http.ResponseWriter,
	request *http.Request,
) {
	if !s.authenticationEnabled() {
		writeError(writer, http.StatusForbidden, ErrNotSupported)

		return
	}

	// Only an empty object is accepted: selection comes from the server's kubeconfig, not the browser.
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxRequestBodyBytes))
	decoder.DisallowUnknownFields()

	var consent struct{}

	err := decoder.Decode(&consent)
	if err != nil {
		writeDecodeError(writer, err)

		return
	}

	service, ok := s.Service.(ClusterAuthenticationService)
	if !ok {
		http.NotFound(writer, request)

		return
	}

	err = service.RenewAuthentication(
		request.Context(),
		request.PathValue("namespace"),
		request.PathValue("name"),
	)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	writer.WriteHeader(http.StatusNoContent)
}
