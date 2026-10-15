// Package systemd lê os serviços systemd do usuário (~/.config/systemd/user).
// O systemd já guarda estado, reinícios e logs, então esses serviços não
// precisam do bw run.
package systemd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Unit é o estado atual de um serviço.
type Unit struct {
	Name        string // sem o sufixo .service
	Description string
	ActiveState string    // active, inactive, failed, activating...
	SubState    string    // running, dead, auto-restart...
	Since       time.Time // quando entrou no estado ativo; zero se nunca subiu
	Until       time.Time // quando saiu do estado ativo pela última vez
	ExitStatus  int       // exit code da última vez que o processo principal terminou
	Restarts    int       // reinícios automáticos desde o último start manual
}

const properties = "Id,Description,ActiveState,SubState,ActiveEnterTimestamp,InactiveEnterTimestamp,ExecMainStatus,NRestarts"

// Load devolve os serviços definidos em ~/.config/systemd/user. Serviços do
// sistema e de pacotes ficam de fora: não foram criados pelo usuário.
func Load() ([]Unit, error) {
	names, err := userServices()
	if err != nil || len(names) == 0 {
		return nil, err
	}
	args := append([]string{"--user", "show", "--timestamp=unix", "--property", properties}, names...)
	out, err := exec.Command("systemctl", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("systemctl --user show: %w", err)
	}
	return parseShow(string(out)), nil
}

// WriteLogs escreve em w as últimas lines linhas do journal do serviço.
func WriteLogs(w io.Writer, name string, lines int) error {
	cmd := exec.Command("journalctl", "--user", "--unit", name+".service",
		"--lines", strconv.Itoa(lines), "--no-pager")
	cmd.Stdout, cmd.Stderr = w, w
	return cmd.Run()
}

func userServices() ([]string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(configDir, "systemd", "user"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		// "x@.service" é template: só roda instanciado, com outro nome.
		if !e.IsDir() && strings.HasSuffix(name, ".service") && !strings.HasSuffix(name, "@.service") {
			names = append(names, name)
		}
	}
	return names, nil
}

// parseShow lê a saída de "systemctl show": um bloco "chave=valor" por
// serviço, separado por linha em branco, sem ordem fixa de chaves.
func parseShow(text string) []Unit {
	var units []Unit
	for block := range strings.SplitSeq(strings.TrimSpace(text), "\n\n") {
		props := map[string]string{}
		for line := range strings.Lines(block) {
			key, value, _ := strings.Cut(strings.TrimSpace(line), "=")
			props[key] = value
		}
		if props["Id"] == "" {
			continue
		}
		units = append(units, Unit{
			Name:        strings.TrimSuffix(props["Id"], ".service"),
			Description: props["Description"],
			ActiveState: props["ActiveState"],
			SubState:    props["SubState"],
			Since:       parseTimestamp(props["ActiveEnterTimestamp"]),
			Until:       parseTimestamp(props["InactiveEnterTimestamp"]),
			ExitStatus:  atoi(props["ExecMainStatus"]),
			Restarts:    atoi(props["NRestarts"]),
		})
	}
	return units
}

// parseTimestamp lê o formato de --timestamp=unix ("@1792069539"). Vazio
// significa que o serviço nunca passou pelo estado.
func parseTimestamp(value string) time.Time {
	secs, err := strconv.ParseInt(strings.TrimPrefix(value, "@"), 10, 64)
	if err != nil || secs == 0 {
		return time.Time{}
	}
	return time.Unix(secs, 0)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
