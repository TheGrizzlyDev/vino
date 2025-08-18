package path

import (
	"fmt"
	"path/filepath"
	"strings"
)

type PathErrorKind string

const (
	ErrEmpty       PathErrorKind = "empty"
	ErrInvalidUNC  PathErrorKind = "invalid_unc"
	ErrUnsupported PathErrorKind = "unsupported"
)

type PathError struct {
	Kind PathErrorKind
	Path string
}

func (e *PathError) Error() string {
	return fmt.Sprintf("TranslatePathToWine error (%s): %q", e.Kind, e.Path)
}

// TranslatePathToWine converts a Windows path into a Unix path under a Wine
// prefix *purely by convention*, without touching the filesystem.
//
// Rules:
//   - Drive paths:  C:\x → <prefix>/drive_c/x
//   - UNC paths:    \\s\sh\p → <prefix>/dosdevices/unc/s/sh/p
//   - Extended:     \\?\C:\x and \\?\UNC\... supported
//   - Mixed slashes are OK; dot segments are cleaned
//
// Returns *PathError on failure.
func TranslatePathToWine(winePrefix, windowsPath string) (string, error) {
	win := strings.TrimSpace(windowsPath)
	if win == "" {
		return "", &PathError{Kind: ErrEmpty, Path: windowsPath}
	}
	// Normalize to forward slashes for parsing
	win = strings.ReplaceAll(win, `\`, `/`)

	// Strip extended-length prefix: //?/C:/... or //?/UNC/...
	if strings.HasPrefix(win, "//?/") {
		win = strings.TrimPrefix(win, "//?/")
		// Handle //?/UNC/server/share/... → //server/share/...
		if len(win) >= 4 && strings.EqualFold(win[:4], "UNC/") {
			win = "//" + win[4:]
		}
	}

	// UNC: //server/share/...
	if strings.HasPrefix(win, "//") {
		rest := strings.TrimPrefix(win, "//")
		parts := splitNonEmpty(rest, "/")
		if len(parts) < 2 {
			return "", &PathError{Kind: ErrInvalidUNC, Path: windowsPath}
		}
		server, share := parts[0], parts[1]
		sub := filepath.Join(parts[2:]...)
		return cleanJoin(winePrefix, "dosdevices", "unc", server, share, sub), nil
	}

	// Drive-qualified: C:/..., c:/..., also C:foo (treated as C:/foo)
	if looksLikeDrivePath(win) {
		drive := strings.ToLower(win[:1]) // 'c'
		rest := strings.TrimPrefix(win[1:], ":")
		if !strings.HasPrefix(rest, "/") {
			rest = "/" + rest
		}
		return cleanJoin(winePrefix, "drive_"+drive, filepath.FromSlash(rest)), nil
	}

	return "", &PathError{Kind: ErrUnsupported, Path: windowsPath}
}

func looksLikeDrivePath(s string) bool {
	if len(s) < 2 {
		return false
	}
	c := s[0]
	if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
		return false
	}
	return s[1] == ':'
}

func splitNonEmpty(s, sep string) []string {
	raw := strings.Split(s, sep)
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		if p != "" && p != "." {
			out = append(out, p)
		}
	}
	return out
}

func cleanJoin(elem ...string) string {
	return filepath.Clean(filepath.Join(elem...))
}
