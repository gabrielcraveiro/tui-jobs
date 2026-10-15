// Package runs guarda o histórico de execuções em disco: um .json de
// metadados e um .log de saída por execução, em <dir>/<job>/.
//
// Um arquivo por execução evita lock entre jobs que o cron dispara no mesmo
// minuto e deixa o histórico legível com ls e cat.
package runs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NamePattern é o formato aceito para nome de job. O nome vira diretório,
// então barra e espaço ficam de fora.
const NamePattern = `[A-Za-z0-9][A-Za-z0-9._-]*`

var validName = regexp.MustCompile(`^` + NamePattern + `$`)

// Run é uma execução de um job. End fica nil enquanto o processo roda.
type Run struct {
	Job      string     `json:"job"`
	Command  []string   `json:"command"`
	PID      int        `json:"pid"`
	BootID   string     `json:"boot_id"`
	Start    time.Time  `json:"start"`
	End      *time.Time `json:"end,omitempty"`
	ExitCode int        `json:"exit_code"`
	Error    string     `json:"error,omitempty"`

	id string // nome base dos arquivos, sem extensão
}

// Duration devolve quanto a execução levou, ou há quanto tempo ela roda.
func (r Run) Duration(now time.Time) time.Duration {
	if r.End != nil {
		return r.End.Sub(r.Start)
	}
	return now.Sub(r.Start)
}

// Store é o diretório do histórico.
type Store struct {
	dir string
}

func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// DefaultDir devolve onde o histórico fica: $BOTWATCHER_DIR,
// $XDG_STATE_HOME/botwatcher ou ~/.local/state/botwatcher. Sem nenhum deles
// devolve erro em vez de cair num caminho fixo do /tmp: outro usuário poderia
// criar esse caminho antes (ou um symlink nele) e ler os logs.
func DefaultDir() (string, error) {
	if dir := os.Getenv("BOTWATCHER_DIR"); dir != "" {
		return dir, nil
	}
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "botwatcher"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no history directory, set HOME or BOTWATCHER_DIR: %w", err)
	}
	return filepath.Join(home, ".local", "state", "botwatcher"), nil
}

// Begin grava o início de uma execução e abre o arquivo de log dela.
// Quem chama fecha o log e chama Finish.
func (s *Store) Begin(job string, command []string) (*Run, *os.File, error) {
	if err := checkName(job); err != nil {
		return nil, nil, err
	}
	// Logs podem conter tokens dos bots, então só o dono lê.
	if err := os.MkdirAll(filepath.Join(s.dir, job), 0o700); err != nil {
		return nil, nil, err
	}
	now := time.Now()
	r := &Run{
		Job:     job,
		Command: command,
		PID:     os.Getpid(),
		BootID:  bootID(),
		Start:   now,
		// Horário UTC com largura fixa no começo: ordem alfabética = cronológica.
		id: now.UTC().Format("20060102T150405.000000Z") + "-" + strconv.Itoa(os.Getpid()),
	}
	logFile, err := os.OpenFile(s.LogPath(*r), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, nil, err
	}
	if err := s.save(r); err != nil {
		logFile.Close()
		return nil, nil, err
	}
	return r, logFile, nil
}

// Finish grava o fim da execução.
func (s *Store) Finish(r *Run, exitCode int, errMsg string) error {
	end := time.Now()
	r.End, r.ExitCode, r.Error = &end, exitCode, errMsg
	return s.save(r)
}

// Jobs lista os jobs que têm histórico, em ordem alfabética.
func (s *Store) Jobs() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && validName.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// List devolve as execuções de um job, da mais recente para a mais antiga.
// Com limit <= 0 devolve todas.
func (s *Store) List(job string, limit int) ([]Run, error) {
	if err := checkName(job); err != nil {
		return nil, err
	}
	ids, err := s.ids(job)
	if err != nil {
		return nil, err
	}
	slices.Reverse(ids)

	var list []Run
	for _, id := range ids {
		if limit > 0 && len(list) == limit {
			break
		}
		var r Run
		data, err := os.ReadFile(s.path(job, id, ".json"))
		if err == nil {
			err = json.Unmarshal(data, &r)
		}
		if err != nil {
			// Arquivo estragado, ou apagado agora pelo Prune de outro bw run:
			// não pode derrubar o monitor inteiro.
			continue
		}
		r.id = id
		list = append(list, r)
	}
	return list, nil
}

// Prune apaga as execuções mais antigas e mantém as keep mais recentes.
func (s *Store) Prune(job string, keep int) error {
	ids, err := s.ids(job)
	if err != nil || len(ids) <= keep {
		return err
	}
	var errs []error
	for _, id := range ids[:len(ids)-keep] {
		for _, ext := range []string{".json", ".log"} {
			// Dois bw run do mesmo job podem limpar ao mesmo tempo.
			if err := os.Remove(s.path(job, id, ext)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// LogPath devolve o arquivo com stdout+stderr da execução.
func (s *Store) LogPath(r Run) string {
	return s.path(r.Job, r.id, ".log")
}

// Tail devolve no máximo os últimos maxBytes do log da execução. Bot de longa
// duração gera log grande, e a tela só mostra o fim.
func (s *Store) Tail(r Run, maxBytes int64) (string, error) {
	f, err := os.Open(s.LogPath(r))
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	offset := max(info.Size()-maxBytes, 0)
	data := make([]byte, info.Size()-offset)
	if _, err := f.ReadAt(data, offset); err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if offset > 0 {
		// A primeira linha quase sempre vem cortada no meio.
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	return string(data), nil
}

// Alive diz se o processo do bw run de r ainda existe. O boot ID vem antes do
// PID porque, depois que o WSL reinicia, o mesmo PID pode ser de outro processo.
func Alive(r Run) bool {
	if r.BootID != bootID() {
		return false
	}
	_, err := os.Stat("/proc/" + strconv.Itoa(r.PID))
	return err == nil
}

var bootID = sync.OnceValue(func() string {
	data, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return strings.TrimSpace(string(data))
})

func checkName(job string) error {
	if !validName.MatchString(job) {
		return fmt.Errorf("invalid job name %q: use letters, digits, '.', '_' or '-'", job)
	}
	return nil
}

// ids devolve os nomes base das execuções de um job, da mais antiga para a
// mais recente.
func (s *Store) ids(job string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, job))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), ".json"); ok {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (s *Store) path(job, id, ext string) string {
	return filepath.Join(s.dir, job, id+ext)
}

// save grava via arquivo temporário + rename para quem lê nunca ver um JSON
// pela metade.
func (s *Store) save(r *Run) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	path := s.path(r.Job, r.id, ".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
