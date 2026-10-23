package main

import (
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string // "" means unset/empty
		want    config
		wantErr string // substring of the expected error, "" for success
	}{
		{
			name: "all set",
			env:  map[string]string{"DB_URL": "postgres://x", "HTTP_ADDR": ":8080", "RUN_MIGRATIONS": "true"},
			want: config{databaseURL: "postgres://x", httpAddr: ":8080", runMigrations: true},
		},
		{
			name: "migrations default to false",
			env:  map[string]string{"DB_URL": "postgres://x", "HTTP_ADDR": ":8080", "RUN_MIGRATIONS": ""},
			want: config{databaseURL: "postgres://x", httpAddr: ":8080"},
		},
		{
			name:    "missing DB_URL",
			env:     map[string]string{"DB_URL": "", "HTTP_ADDR": ":8080", "RUN_MIGRATIONS": ""},
			wantErr: "DB_URL",
		},
		{
			name:    "missing HTTP_ADDR",
			env:     map[string]string{"DB_URL": "postgres://x", "HTTP_ADDR": "", "RUN_MIGRATIONS": ""},
			wantErr: "HTTP_ADDR",
		},
		{
			name:    "invalid RUN_MIGRATIONS",
			env:     map[string]string{"DB_URL": "postgres://x", "HTTP_ADDR": ":8080", "RUN_MIGRATIONS": "yes please"},
			wantErr: "RUN_MIGRATIONS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// t.Setenv restores the original values when the test ends.
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			got, err := loadConfig()

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want error mentioning %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("config = %+v, want %+v", got, tt.want)
			}
		})
	}
}
