//go:build !linux

package host

import (
	"context"
	"errors"
)

type SunshineAdmin struct{}

type SunshineClient struct {
	Name    string `json:"name"`
	UUID    string `json:"uuid"`
	Enabled bool   `json:"enabled"`
}

func NewSunshineAdmin(string, string, string, string) (*SunshineAdmin, error) {
	return nil, errors.New("sunshine_admin_unsupported")
}

func (*SunshineAdmin) ListClients(context.Context) ([]SunshineClient, error) {
	return nil, errors.New("sunshine_admin_unsupported")
}

func (*SunshineAdmin) SetClientEnabled(context.Context, string, bool) error {
	return errors.New("sunshine_admin_unsupported")
}

func (*SunshineAdmin) UnpairClient(context.Context, string) error {
	return errors.New("sunshine_admin_unsupported")
}
