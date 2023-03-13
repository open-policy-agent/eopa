package lia

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	padding  = 2
	maxWidth = 80
	debug    = false
)

func TUI(ctx context.Context, rec Recorder) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if debug {
		log.SetFlags(log.Lshortfile)
		if f, err := tea.LogToFile("debug.log", "debug"); err != nil {
			panic(err)
		} else {
			defer f.Close()
		}
	}

	p := tea.NewProgram(model{
		progress: progress.New(progress.WithDefaultGradient()),
		rec:      rec,
		ctx:      ctx,
		cancel:   cancel,
	}, tea.WithOutput(os.Stderr))

	m, err := p.Run()
	if err != nil {
		return err
	}
	if buf := m.(model).reportBuf; buf != nil {
		_, err := io.Copy(os.Stdout, buf)
		return err
	}
	return nil
}

func (m model) startRecorder() tea.Msg {
	rep, err := m.rec.Run(m.ctx)
	if err != nil {
		return err
	}
	return startedMsg{
		report: rep,
		begin:  time.Now(),
	}
}

type model struct {
	progress  progress.Model
	rec       Recorder
	report    Report
	err       error
	begin     time.Time
	reportBuf *bytes.Buffer
	ctx       context.Context
	cancel    context.CancelFunc
}

func (m model) Init() tea.Cmd {
	return m.startRecorder
}

type (
	tickMsg    time.Time
	startedMsg struct {
		report Report
		begin  time.Time
	}
	outputMsg struct {
		buf *bytes.Buffer
	}

	doneMsg struct{}
)

func l(m tea.Msg) {
	if !debug {
		return
	}
	log.Printf("received %T %[1]v", m)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.progress.Width = msg.Width - padding*2 - 4
		if m.progress.Width > maxWidth {
			m.progress.Width = maxWidth
		}
		return m, nil

	case startedMsg:
		l(msg)
		m.begin = msg.begin
		m.report = msg.report
		return m, tea.Batch(tickCmd(), outputCmd(m.ctx, m.rec, msg.report))

	case error:
		l(msg)
		m.err = msg
		return m, tea.Quit

	case outputMsg:
		l(msg)
		m.reportBuf = msg.buf
		return m, nil

	case doneMsg:
		l(msg)
		return m, nil

	case tickMsg:
		l(msg)
		if time.Time(msg).After(m.begin.Add(m.rec.Duration())) {
			return m, tea.Quit
		}
		return m, tickCmd()

	case progress.FrameMsg: // FrameMsg is sent when the progress bar wants to animate itself
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.cancel()
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m model) View() string {
	pad := strings.Repeat(" ", padding)
	switch {
	case m.err != nil:
		return pad + "Error: " + m.err.Error() + "\n"

	case m.begin.IsZero():
		return pad + "Uploading bundle...\n"

	default:
		delta := float64(time.Since(m.begin)) / float64(m.rec.Duration())
		return pad + m.progress.ViewAs(float64(delta)) + "\nPress Ctrl+C to cancel"
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func outputCmd(ctx context.Context, rec Recorder, rep Report) tea.Cmd {
	if rec.ToStdout() {
		return func() tea.Msg {
			buf := bytes.Buffer{}
			if err := rep.Output(ctx, &buf, rec.Format()); err != nil {
				return err
			}
			return outputMsg{&buf}
		}
	}
	return func() tea.Msg {
		if err := rec.Output(ctx, rep); err != nil {
			return err
		}
		return doneMsg{}
	}
}
