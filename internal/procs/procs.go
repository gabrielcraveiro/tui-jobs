// Package procs mede a memória de processos vivos pelo /proc.
//
// A medida é RSS, o mesmo número que o htop mostra. Somar RSS de vários
// processos conta duas vezes a memória compartilhada (bibliotecas), mas é o
// número que o usuário reconhece e compara.
package procs

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Snapshot é uma leitura do /proc num instante: pai e RSS de cada processo.
type Snapshot struct {
	cgroupRoot string
	children   map[int][]int
	rss        map[int]int64 // em bytes
}

// Read lê os processos do sistema.
func Read() Snapshot {
	return ReadFrom("/proc", "/sys/fs/cgroup")
}

// ReadFrom lê os processos de procRoot; cgroupRoot é onde os cgroups estão
// montados. Processo que some no meio da leitura fica de fora.
func ReadFrom(procRoot, cgroupRoot string) Snapshot {
	s := Snapshot{cgroupRoot: cgroupRoot, children: map[int][]int{}, rss: map[int]int64{}}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return s
	}
	pageSize := int64(os.Getpagesize())
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // não é diretório de processo
		}
		ppid, pages, ok := readStat(filepath.Join(procRoot, e.Name(), "stat"))
		if !ok {
			continue
		}
		s.children[ppid] = append(s.children[ppid], pid)
		s.rss[pid] = pages * pageSize
	}
	return s
}

// Descendants soma o RSS de todos os descendentes de pid, sem o próprio pid:
// o pid do bw run não é memória do job.
func (s Snapshot) Descendants(pid int) int64 {
	var total int64
	// Cópia: a pilha cresce com append e não pode escrever no índice.
	pending := slices.Clone(s.children[pid])
	for len(pending) > 0 {
		next := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		total += s.rss[next]
		pending = append(pending, s.children[next]...)
	}
	return total
}

// Cgroup soma o RSS dos processos de um cgroup v2. path é o caminho que o
// systemd informa em ControlGroup.
func (s Snapshot) Cgroup(path string) int64 {
	f, err := os.Open(filepath.Join(s.cgroupRoot, path, "cgroup.procs"))
	if err != nil {
		return 0
	}
	defer f.Close()
	var total int64
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		if pid, err := strconv.Atoi(strings.TrimSpace(lines.Text())); err == nil {
			total += s.rss[pid]
		}
	}
	return total
}

// readStat devolve o pai e o RSS (em páginas) de /proc/<pid>/stat. O nome do
// processo vem entre parênteses e pode ter espaço e parêntese, então os
// campos são contados a partir do último ")".
func readStat(path string) (ppid int, rssPages int64, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, false
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return 0, 0, false
	}
	// Depois do ")": estado (campo 3), ppid (4) ... rss (24).
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 22 {
		return 0, 0, false
	}
	ppid, err1 := strconv.Atoi(fields[1])
	rssPages, err2 := strconv.ParseInt(fields[21], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return ppid, rssPages, true
}
