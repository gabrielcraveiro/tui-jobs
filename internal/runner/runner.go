// Package runner executa o comando de um job e registra a execução.
package runner

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"

	"github.com/gabrielcraveiro/tui-jobs/internal/runs"
)

// keepRuns limita o histórico por job: dá para ver padrões de falha sem
// deixar o diretório crescer para sempre.
const keepRuns = 50

// Exec roda command, espelha a saída no stdout/stderr e no log da execução, e
// devolve o exit code que o bw deve usar. Com store nil, roda sem registrar.
func Exec(store *runs.Store, job string, command []string, stdout, stderr io.Writer) int {
	if store == nil {
		return executeUnrecorded(command, stdout, stderr)
	}
	run, logFile, err := store.Begin(job, command)
	if err != nil {
		// O job importa mais que o histórico: sem conseguir registrar, roda mesmo assim.
		fmt.Fprintf(stderr, "bw: cannot record run: %v\n", err)
		return executeUnrecorded(command, stdout, stderr)
	}
	defer logFile.Close()

	// Espelhar no stdout mantém funcionando os ">> arquivo.log" que já existem no crontab.
	res, waitOutput := execute(command, io.MultiWriter(stdout, logFile), io.MultiWriter(stderr, logFile))
	errMsg := ""
	if res.err != nil {
		errMsg = res.err.Error()
	}
	if err := store.Finish(run, res.code, res.peakRSS, errMsg); err != nil {
		fmt.Fprintf(stderr, "bw: cannot record end of run: %v\n", err)
	}
	// O fim do job já está gravado. Um processo que ele deixou em background
	// pode ter herdado a saída: o bw segue copiando para o log até ela fechar,
	// como o cron faz.
	waitOutput()
	if res.err != nil {
		fmt.Fprintf(io.MultiWriter(stderr, logFile), "bw: %v\n", res.err)
	}
	if err := store.Prune(job, keepRuns); err != nil {
		fmt.Fprintf(stderr, "bw: cannot prune old runs: %v\n", err)
	}
	return res.code
}

func executeUnrecorded(command []string, stdout, stderr io.Writer) int {
	res, waitOutput := execute(command, stdout, stderr)
	waitOutput()
	if res.err != nil {
		fmt.Fprintf(stderr, "bw: %v\n", res.err)
	}
	return res.code
}

// result é o que o bw sabe quando o processo principal sai.
type result struct {
	// code segue o padrão do shell: 128+N quando o processo morre pelo sinal
	// N e 127 quando o comando nem chega a iniciar.
	code int
	// peakRSS é o maior RSS entre o processo e os filhos que ele esperou, em
	// bytes. O kernel guarda o pico, então vale até para job de segundos.
	peakRSS int64
	err     error
}

// execute espera só o processo principal. A cópia da saída continua até todos
// os processos que a herdaram fecharem; waitOutput espera esse fim.
func execute(command []string, stdout, stderr io.Writer) (res result, waitOutput func()) {
	var copying sync.WaitGroup
	var mu sync.Mutex
	outW, err := pipeTo(lockedWriter{&mu, stdout}, &copying)
	if err != nil {
		return result{code: 1, err: err}, copying.Wait
	}
	errW, err := pipeTo(lockedWriter{&mu, stderr}, &copying)
	if err != nil {
		outW.Close()
		return result{code: 1, err: err}, copying.Wait
	}

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, outW, errW
	startErr := cmd.Start()
	// A ponta de escrita fica só com o processo e quem herdar dele. Sem fechar
	// a nossa cópia, a leitura nunca chegaria ao fim.
	outW.Close()
	errW.Close()
	if startErr != nil {
		return result{code: 127, err: startErr}, copying.Wait
	}

	stop := forwardSignals(cmd.Process)
	err = cmd.Wait()
	stop()
	if cmd.ProcessState == nil {
		return result{code: 1, err: err}, copying.Wait
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		err = nil // exit code diferente de zero é resultado do job, não erro do bw
	}
	res = result{code: cmd.ProcessState.ExitCode(), err: err}
	if usage, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
		res.peakRSS = usage.Maxrss * 1024 // no Linux, Maxrss vem em KiB
	}
	if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		res.code = 128 + int(status.Signal())
	}
	return res, copying.Wait
}

// pipeTo devolve a ponta de escrita de um pipe copiado para dst. Com um
// *os.File no Stdout, o Wait do exec volta quando o processo sai, em vez de
// esperar todo mundo que herdou a saída.
func pipeTo(dst io.Writer, copying *sync.WaitGroup) (*os.File, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	copying.Go(func() {
		defer r.Close()
		_, _ = io.Copy(dst, r)
	})
	return w, nil
}

// lockedWriter serializa as cópias de stdout e stderr, que podem cair no
// mesmo destino.
type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// forwardSignals repassa SIGTERM e SIGHUP ao filho, para que um "kill" no bw
// pare o job e o fim ainda seja gravado. SIGINT só é capturado: no terminal o
// Ctrl+C já chega ao filho pelo grupo de processos, e repassar entregaria o
// sinal duas vezes.
func forwardSignals(child *os.Process) (stop func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-signals:
				if sig != syscall.SIGINT {
					_ = child.Signal(sig)
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
	}
}
