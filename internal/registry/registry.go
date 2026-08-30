// Package registry talks to container registries.
//
// It uses go-containerregistry rather than the Docker API, so no daemon has to
// be running and credentials come from the same ~/.docker/config.json the
// docker CLI reads.
package registry

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// Client is the registry surface the updater needs. It is an interface so
// tests can run against an in-process registry instead of the network.
type Client interface {
	// Digest resolves a reference to the digest of the manifest it points at.
	Digest(ctx context.Context, ref string) (string, error)
	// Tags lists the tags in a reference's repository.
	Tags(ctx context.Context, ref string) ([]string, error)
}

// Remote is a Client backed by real registries.
type Remote struct {
	opts []remote.Option
}

// New returns a Remote authenticating the way the docker CLI does.
func New() *Remote {
	return &Remote{opts: []remote.Option{
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	}}
}

// NewWithOptions returns a Remote with explicit options, for tests pointing at
// a local registry.
func NewWithOptions(opts ...remote.Option) *Remote {
	return &Remote{opts: opts}
}

// Digest resolves a reference to the digest of the manifest it points at.
func (r *Remote) Digest(ctx context.Context, ref string) (string, error) {
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return "", err
	}
	desc, err := remote.Get(parsed, append(r.opts, remote.WithContext(ctx))...)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", ref, err)
	}
	return desc.Digest.String(), nil
}

// Tags lists the tags in a reference's repository.
func (r *Remote) Tags(ctx context.Context, ref string) ([]string, error) {
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return nil, err
	}
	tags, err := remote.List(parsed.Context(), append(r.opts, remote.WithContext(ctx))...)
	if err != nil {
		return nil, fmt.Errorf("listing tags for %s: %w", parsed.Context().Name(), err)
	}
	return tags, nil
}
