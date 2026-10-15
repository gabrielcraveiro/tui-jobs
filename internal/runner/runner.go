// Package runner executa o comando de um job e registra a execução.
package runner

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/gabrielcraveiro/tui-jobs/internal/runs"
)

// keepRuns limita o histórico por job: dá para ver padrões de falha sem
// deixar o diretório crescer para sempre.
const keepRuns = 50

// Exec roda command, espelha a saída no stdout/stderr e no log da execução, e
// devolve o exit code que o bw deve usar.
func Exec(store *runs.Store, job string, command []string, stdout, stderr io.Writer) int {
	run, logFile, err := store.Begin(job, command)
	if err != nil {
		// O job importa mais que o histórico: sem conseguir registrar, roda mesmo assim.
		fmt.Fprintf(stderr, "bw: cannot record run: %v\n", err)
		code, err := execute(command, stdout, stderr)
		if err != nil {
			fmt.Fprintf(stderr, "bw: %v\n", err)
		}
		return code
	}
	defer logFile.Close()

	// Espelhar no stdout mantém funcionando os ">> arquivo.log" que já existem no crontab.
	code, execErr := execute(command, io.MultiWriter(stdout, logFile), io.MultiWriter(stderr, logFile))
	errMsg := ""
	if execErr != nil {
		errMsg = execErr.Error()
		fmt.Fprintf(io.MultiWriter(stderr, logFile), "bw: %v\n", execErr)
	}
	if err := store.Finish(run, code, errMsg); err != nil {
		fmt.Fprintf(stderr, "bw: cannot record end of run: %v\n", err)
	}
	if err := store.Prune(job, keepRuns); err != nil {
		fmt.Fprintf(stderr, "bw: cannot prune old runs: %v\n", err)
	}
	return code
}

// execute devolve o exit code no padrão do shell: 128+N quando o processo
// morre pelo sinal N e 127 quando o comando nem chega a iniciar.
func execute(command []string, stdout, stderr io.Writer) (int, error) {
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
	if err := cmd.Start(); err != nil {
		return 127, err
	}
	stop := forwardSignals(cmd.Process)
	defer stop()

	err := cmd.Wait()
	if cmd.ProcessState == nil {
		return 1, err
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		err = nil // exit code diferente de zero é resultado do job, não erro do bw
	}
	if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal()), err
	}
	return cmd.ProcessState.ExitCode(), err
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
