// SPDX-License-Identifier: AGPL-3.0-or-later

// Package install installe le binaire courant pour l'utilisateur, sans
// droits root : ~/.local/bin, lanceur d'application et démarrage automatique.
package install

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed xorway.desktop
var desktopTemplate string

// Desktop renvoie le fichier .desktop pour un binaire installé dans binDir.
func Desktop(binDir string) string {
	return strings.ReplaceAll(desktopTemplate, "@BINDIR@", binDir)
}

// paths regroupe les emplacements d'installation de l'utilisateur.
type paths struct {
	bin, app, autostart string
}

func userPaths() (paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return paths{}, err
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		return paths{}, err
	}
	return paths{
		bin:       filepath.Join(home, ".local", "bin", "xorway"),
		app:       filepath.Join(data, "applications", "xorway.desktop"),
		autostart: filepath.Join(cfg, "autostart", "xorway.desktop"),
	}, nil
}

// Install copie l'exécutable courant dans ~/.local/bin et crée les fichiers
// .desktop. Elle écrit sur w ce qu'elle fait.
func Install(w io.Writer, autostart bool) error {
	p, err := userPaths()
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("emplacement de l'exécutable : %w", err)
	}
	if self, err = filepath.EvalSymlinks(self); err != nil {
		return err
	}

	if self != p.bin {
		if err := copyFile(self, p.bin, 0o755); err != nil {
			return fmt.Errorf("copie du binaire : %w", err)
		}
	}
	fmt.Fprintln(w, "binaire     :", p.bin)

	desktop := Desktop(filepath.Dir(p.bin))
	if err := writeFile(p.app, desktop); err != nil {
		return err
	}
	fmt.Fprintln(w, "lanceur     :", p.app)

	if autostart {
		if err := writeFile(p.autostart, desktop); err != nil {
			return err
		}
		fmt.Fprintln(w, "autostart   :", p.autostart)
	} else if err := remove(p.autostart); err != nil {
		return err
	}
	return nil
}

// Uninstall supprime les fichiers créés par Install. La configuration
// (~/.config/xorway) est conservée.
func Uninstall(w io.Writer) error {
	p, err := userPaths()
	if err != nil {
		return err
	}
	for _, f := range []string{p.autostart, p.app, p.bin} {
		if err := remove(f); err != nil {
			return err
		}
		fmt.Fprintln(w, "supprimé    :", f)
	}
	return nil
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// copyFile copie via un fichier temporaire puis un renommage, ce qui
// fonctionne même si une ancienne version du binaire est en cours d'exécution.
func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".xorway-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // sans effet après le renommage
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}
