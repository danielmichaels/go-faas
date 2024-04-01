package faas

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestValidateMethod(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		expectErr bool
	}{
		{
			name:      "test GET method",
			method:    http.MethodGet,
			expectErr: false,
		},
		{
			name:      "test POST method",
			method:    http.MethodPost,
			expectErr: false,
		},
		{
			name:      "test unsupported method",
			method:    http.MethodPut,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, "http://example.com", nil)
			if err != nil {
				t.Fatalf("could not create request: %v", err)
			}

			err = validateMethod(req)

			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected an error but didn't get one")
				}
			} else if err != nil {
				t.Fatalf("didn't expected an error but got one: %v", err)
			}
		})
	}
}

func TestValidateCORS(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		origin     string
		shouldPass bool
	}{
		{
			name:       "Options method with valid origin",
			method:     "OPTIONS",
			origin:     "http://valid.com",
			shouldPass: true,
		},
		{
			name:       "Options method with invalid origin",
			method:     "OPTIONS",
			origin:     "http://invalid.com",
			shouldPass: false,
		},
		{
			name:       "Non-options method with valid origin",
			method:     "GET",
			origin:     "http://valid.com",
			shouldPass: true,
		},
		{
			name:       "Non-methods method with invalid origin",
			method:     "GET",
			origin:     "http://invalid.com",
			shouldPass: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, "/", nil)
			if err != nil {
				t.Fatal(err)
			}

			req.Header.Set("Origin", tc.origin)
			resp := httptest.NewRecorder()
			origins := []string{"http://valid.com"}

			err = validateCORS(resp, req, origins)
			if err != nil && tc.shouldPass {
				t.Errorf("unexpected error: %v", err)
			}

			if tc.shouldPass {
				if req.Method == "OPTIONS" && resp.Header().Get("Access-Control-Allow-Headers") != "Authorization" {
					t.Errorf("Expected Authorization in Access-Control-Allow-Headers but got %s",
						resp.Header().Get("Access-Control-Allow-Headers"))
				}

				if resp.Header().Get("Access-Control-Allow-Methods") != "GET,POST,OPTIONS" {
					t.Errorf("Expected GET,POST,OPTIONS in Access-Control-Allow-Methods but got %s",
						resp.Header().Get("Access-Control-Allow-Methods"))
				}

				if resp.Header().Get("Access-Control-Allow-Origin") != tc.origin {
					t.Errorf("Expected %s in Access-Control-Allow-Origin but got %s",
						tc.origin, resp.Header().Get("Access-Control-Allow-Origin"))
				}
			} else {
				if resp.Header().Get("Access-Control-Allow-Origin") == tc.origin {
					t.Errorf("Expected different origin in Access-Control-Allow-Origin but got %s",
						tc.origin)
				}
			}
		})
	}
}

func TestGetEnvOrError(t *testing.T) {
	testCases := []struct {
		name        string
		envVariabe  string
		expectedErr string
		expectedVal string
	}{
		{
			name:        "Exist Environment Variable",
			envVariabe:  "PATH",
			expectedErr: "",
			expectedVal: os.Getenv("PATH"),
		},
		{
			name:        "Non-Exist Environment Variable",
			envVariabe:  "NON_EXIST_ENV",
			expectedErr: "environment variable not set",
			expectedVal: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			val, err := getEnvOrError(tc.envVariabe)
			if val != tc.expectedVal || (err != nil && err.Error() != tc.expectedErr) {
				t.Fatalf("expect '%s' and '%s', got '%s' and '%v'", tc.expectedVal, tc.expectedErr, val, err)
			}
		})
	}
}
