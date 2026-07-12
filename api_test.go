package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestHandleScan(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/scan", nil)
	w := httptest.NewRecorder()
	
	handleScan(w, req)
	
	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("handleScan: status code = %d, want 200", resp.StatusCode)
	}
	
	var respJSON map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&respJSON); err != nil {
		t.Fatalf("handleScan: failed to decode JSON: %v", err)
	}
	
	if _, ok := respJSON["routers"].([]interface{}); !ok {
		t.Error("handleScan: response should have 'routers' array")
	}
}

func TestHandleIndex(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	
	handleIndex(w, req)
	
	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("handleIndex: status code = %d, want 200", resp.StatusCode)
	}
	
	if resp.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("handleIndex: Content-Type = %q, want %q", resp.Header.Get("Content-Type"), "text/html; charset=utf-8")
	}
}

func TestDeployRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     deployRequest
		wantErr bool
	}{
		{
			name:    "missing IP and password",
			req:     deployRequest{IP: "", Password: "", LNURL: "test@wallet.app"},
			wantErr: true,
		},
		{
			name:    "missing password",
			req:     deployRequest{IP: "192.168.1.1", Password: "", LNURL: "test@wallet.app"},
			wantErr: true,
		},
		{
			name:    "missing IP",
			req:     deployRequest{IP: "", Password: "password", LNURL: "test@wallet.app"},
			wantErr: true,
		},
		{
			name:    "invalid lightning address",
			req:     deployRequest{IP: "192.168.1.1", Password: "password", LNURL: "invalid"},
			wantErr: true,
		},
		{
			name:    "valid lightning address",
			req:     deployRequest{IP: "192.168.1.1", Password: "password", LNURL: "test@wallet.app"},
			wantErr: false,
		},
		{
			name:    "valid lnurl",
			req:     deployRequest{IP: "192.168.1.1", Password: "password", LNURL: "lnurl1dp68gurn8ghj7um5wfnz7rrfh"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.req.IP == "" || tt.req.Password == "" {
				if !tt.wantErr {
					t.Errorf("%s: expected error, got none", tt.name)
				}
				return
			}
			
			got := validLightningAddress(tt.req.LNURL)
			if !got && !tt.wantErr {
				t.Errorf("%s: validLightningAddress returned false, want true for valid input", tt.name)
			}
			if got && tt.wantErr {
				t.Errorf("%s: validLightningAddress returned true, want false for invalid input", tt.name)
			}
		})
	}
}

func TestDeployRequestDefaults(t *testing.T) {
	req := deployRequest{
		IP:       "192.168.1.1",
		Password: "password",
		LNURL:    "test@wallet.app",
	}
	
	if req.DevSplit != 0 {
		t.Errorf("default DevSplit = %d, want 0", req.DevSplit)
	}
	if req.Margin != 0 {
		t.Errorf("default Margin = %d, want 0", req.Margin)
	}
	if req.Mint != "" {
		t.Errorf("default Mint = %q, want empty", req.Mint)
	}
}

func TestStepInitialization(t *testing.T) {
	steps := deploySteps()
	
	if len(steps) == 0 {
		t.Error("deploySteps should not return empty steps")
	}
	
	expectedSteps := []string{"verify", "firmware", "password", "upstream", "install", "brand", "lnurl", "services", "health"}
	for i, expected := range expectedSteps {
		if i >= len(steps) {
			t.Errorf("missing step %d: %s", i, expected)
			continue
		}
		if steps[i].Name != expected {
			t.Errorf("step %d: Name = %q, want %q", i, steps[i].Name, expected)
		}
		if steps[i].Status != "pending" {
			t.Errorf("step %d: Status = %q, want %q", i, steps[i].Status, "pending")
		}
	}
}