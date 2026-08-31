package dctx

import (
	"reflect"
	"sort"
	"testing"
)

func TestContextPaths(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		target string
		want   []string
		whole  bool
	}{
		{
			name:  "copying the context whole disables the filter",
			body:  "FROM scratch\nCOPY . /\n",
			whole: true,
		}, {
			name:  "so does copying it to a subdirectory",
			body:  "FROM scratch\nCOPY . /src\n",
			whole: true,
		}, {
			name: "a file source and a directory source",
			body: "FROM scratch\nCOPY keep.txt /keep.txt\nCOPY sub /sub\n",
			want: []string{"keep.txt", "sub"},
		}, {
			name: "ADD contributes, but not from a URL",
			body: "FROM scratch\nADD taplo.toml /tmp/\nADD https://example.com/x.tar /tmp/\n",
			want: []string{"taplo.toml"},
		}, {
			// COPY --from reads another stage's filesystem, never the context.
			name: "COPY --from reads a stage, not the context",
			body: "FROM scratch AS a\nCOPY real /real\nFROM scratch\nCOPY --from=a /real /real\n",
			want: []string{"real"},
		}, {
			// The stage is never built, so its sources are never read.
			name: "an unreachable stage contributes nothing",
			body: "FROM scratch AS tools\nCOPY vendor /vendor\nFROM scratch AS final\nCOPY app /app\n",
			want: []string{"app"},
		}, {
			name:   "unless it is the target",
			body:   "FROM scratch AS tools\nCOPY vendor /vendor\nFROM scratch AS final\nCOPY app /app\n",
			target: "tools",
			want:   []string{"vendor"},
		}, {
			// Reached through FROM, so its sources are read too.
			name: "a base stage is reachable",
			body: "FROM scratch AS base\nCOPY base.txt /base.txt\nFROM base AS final\nCOPY app /app\n",
			want: []string{"app", "base.txt"},
		}, {
			// buildkit keeps stages in a map keyed by name, so a repeated name
			// overwrites the earlier entry and the last one wins. Verified
			// against docker, which builds the second of the two here.
			name:   "a repeated stage name resolves to the last",
			body:   "FROM scratch AS dup\nCOPY first /first\nFROM scratch AS dup\nCOPY second /second\nFROM scratch\nCOPY app /app\n",
			target: "dup",
			want:   []string{"second"},
		}, {
			name:   "a target is matched without regard to case",
			body:   "FROM scratch AS Tools\nCOPY vendor /vendor\nFROM scratch\nCOPY app /app\n",
			target: "tools",
			want:   []string{"vendor"},
		}, {
			name: "RUN never reads the context",
			body: "FROM scratch\nRUN echo hi\n",
			want: []string{},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			paths, whole, err := ContextPaths([]byte(tt.body), tt.target)
			if err != nil {
				t.Fatal(err)
			}
			if whole != tt.whole {
				t.Fatalf("whole = %v, want %v", whole, tt.whole)
			}
			if tt.whole {
				return
			}
			sort.Strings(paths)
			if len(paths) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(paths, tt.want) {
				t.Errorf("paths = %q, want %q", paths, tt.want)
			}
		})
	}
}

func TestContextPathsRejectsAnUnknownTarget(t *testing.T) {
	_, _, err := ContextPaths([]byte("FROM scratch AS a\n"), "nope")
	if err == nil {
		t.Fatal("ContextPaths() = nil error, want a failure for an undefined stage")
	}
}
