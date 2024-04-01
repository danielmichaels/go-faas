package faas

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
)

// Map is an interface for generating a custom untyped JSON object.
type Map map[string]any

// Error is the handler custom error struct for JSON responses
type Error struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	Code   int    `json:"code,omitempty"`
}

// GetIpAddress retrieves the requests remote address.
func GetIpAddress(r *http.Request) string {
	remoteAddr := r.RemoteAddr
	xforward := r.Header.Get("X-Forwarded-For")
	switch {
	case remoteAddr != "":
		return remoteAddr
	case xforward != "":
		return xforward
	default:
		return "no-ip-found"
	}
}

// ValidateMethod checks the http.Request is either GET or POST. Everything else
// returns an error.
func ValidateMethod(r *http.Request) error {
	return validateMethod(r)
}
func validateMethod(r *http.Request) error {
	switch r.Method {
	case http.MethodGet:
		return nil
	case http.MethodPost:
		return nil
	default:
		return errors.New(fmt.Sprint("method not allowed"))
	}
}

// ValidateCORS will check that the request is from a valid origin.
func ValidateCORS(w http.ResponseWriter, r *http.Request, origins []string) error {
	err := validateCORS(w, r, origins)
	if err != nil {
		return err
	}
	return nil
}

func validateCORS(w http.ResponseWriter, r *http.Request, origins []string) error {
	if r.Method == "OPTIONS" {
		for _, origin := range origins {
			if r.Header.Get("Origin") == origin {
				w.Header().Set("Access-Control-Allow-Headers", "Authorization")
				w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
				w.Header().Add("Access-Control-Allow-Origin", origin)
				w.Header().Add("Access-Control-Max-Age", "300")
				w.WriteHeader(http.StatusNoContent)
				return nil
			}
		}
	}

	for _, origin := range origins {
		if r.Header.Get("Origin") == origin {
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
			w.Header().Add("Access-Control-Allow-Origin", origin)
		}
	}
	return nil
}

// GetSecret is a helper to retrieve kubernetes/openfaas secrets from the cluster.
func GetSecret(secretName string) ([]byte, error) {
	return getSecret(secretName)
}
func getSecret(secretName string) ([]byte, error) {
	secret, err := os.ReadFile(fmt.Sprintf("/var/openfaas/secrets/%s", secretName))
	if err != nil {
		return nil, err
	}
	return secret, nil
}

func GetSecretString(secretName string) (string, error) {
	return getSecretString(secretName)
}

// getSecretString returns a secret from openfaas as a string with spaces stripped.
func getSecretString(secretName string) (string, error) {
	byt, err := getSecret(secretName)
	if err != nil {
		return "", err
	}
	secret := strings.TrimSpace(string(byt))
	return secret, nil
}

// GetEnvOrError will return an error if the environment variable is not
// found.
func GetEnvOrError(env string) (string, error) {
	return getEnvOrError(env)
}
func getEnvOrError(env string) (string, error) {
	exists := os.Getenv(env)
	if len(exists) == 0 {
		err := errors.New("environment variable not set")
		return "", err
	}
	return exists, nil
}

// WriteJSON will write a JSON response to the caller.
func WriteJSON(w http.ResponseWriter, status int, data any, headers http.Header) error {
	return writeJSON(w, status, data, headers)
}
func writeJSON(w http.ResponseWriter, status int, data any, headers http.Header) error {
	js, err := json.Marshal(data)
	if err != nil {
		return err
	}

	for k, v := range headers {
		w.Header()[k] = v
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(js)
	return nil
}

// writeJSONError returns a JSON response with a custom error type.
func writeJSONError(w http.ResponseWriter, data Error) error {
	js, err := json.Marshal(data)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(data.Code)
	_, _ = w.Write(js)
	return nil
}

// ReadJSON is helper for trapping errors and return values for JSON related
// handlers
func ReadJSON(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	return readJSON(w, r, dst)
}
func readJSON(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	// Set a max body length. Without this it will accept unlimited size requests
	maxBytes := 1_048_576 // 1MB
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes))

	// Init a Decoder and call DisallowUnknownFields() on it before decoding.
	// This means that JSON from the client will be rejected if it contains keys
	// which do not match the target destination struct. If not implemented,
	// the decoder will silently drop unknown fields - this will raise an error instead.
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	// decode the request body into the target struct/destination
	err := dec.Decode(dst)
	if err != nil {
		// start triaging the various JSON related errors
		var syntaxError *json.SyntaxError
		var unmarshallTypeError *json.UnmarshalTypeError
		var invalidUnmarshallError *json.InvalidUnmarshalError

		switch {
		// Use the errors.As() function to check whether the error has the
		// *json.SyntaxError. If it does, then return a user-readable error
		// message including the location of the problem
		case errors.As(err, &syntaxError):
			return fmt.Errorf("body contains badly-formed JSON (at character %d)", syntaxError.Offset)

		// Decode() can also return an io.ErrUnexpectedEOF for JSON syntax errors. This is
		// checked for with errors.Is() and returns a generic error message to the client.
		case errors.Is(err, io.ErrUnexpectedEOF):
			return errors.New("body contains badly-formed JSON")

		// Wrong JSON types will return an error when they do not match the target destination
		// struct.
		case errors.As(err, &unmarshallTypeError):
			if unmarshallTypeError.Field != "" {
				return fmt.Errorf("body contains incorrect JSON type for field %q", unmarshallTypeError.Field)
			}
			return fmt.Errorf("body contains incorrect JSON type (at character %d)", unmarshallTypeError.Offset)

		// An EOF error will be returned by Decode() if the request body is empty. Use errors.Is()
		// to check for this and return a human-readable error message
		case errors.Is(err, io.EOF):
			return errors.New("body must not be empty")

		// If JSON contains a field which cannot be mapped to the target destination
		// then Decode will return an error message in the format "json: unknown field "<name>""
		// We check for this, extract the field name and interpolate it into an error
		// which is returned to the client
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			fieldName := strings.TrimPrefix(err.Error(), "json: unknown field ")
			return fmt.Errorf("body contains unknown key %s", fieldName)

		// If the request body exceeds maxBytes the decode will fail with a
		// "http: request body too large".
		case err.Error() == "http: request body too large":
			return fmt.Errorf("body must not be larger than %d bytes", maxBytes)

		// A json.InvalidUnmarshallError will be returned if we pass a non-nil pointer
		// to Decode(). We catch and panic, rather than return an error.
		case errors.As(err, &invalidUnmarshallError):
			panic(err)

		// All else fails, return an error as-is
		default:
			return err
		}
	}

	// Call Decode() again, using a pointer to anonymous empty struct as the
	// destination. If the body only has one JSON value then an io.EOF error
	// will be returned. If there is anything else, extra data has been sent
	// and we craft a custom error message back to the client
	err = dec.Decode(&struct{}{})
	if err != io.EOF {
		return errors.New("body must only contain a single JSON value")
	}
	return nil
}

// Background helper accepts an arbitrary function as a parameter.
func Background(fn func()) {
	background(fn)
}
func background(fn func()) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("background", "error", fmt.Errorf("%s", err))
			}
		}()
		wg.Done()
		fn()
	}()
	wg.Wait()
}
