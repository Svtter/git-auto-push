package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/judwhite/go-svc"
	"golang.org/x/sys/windows/svc/eventlog"
)

type Repository struct {
	Path   string `json:"path"`
	Remote string `json:"remote"`
	Branch string `json:"branch"`
}

type Config struct {
	Repositories []Repository `json:"repositories"`
	IsCommit     bool         `json:"isCommit"`
	Interval     int          `json:"interval"`
}

// program implements svc.Service
type program struct {
	wg   sync.WaitGroup
	quit chan struct{}
}

func FileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

func CreateFile(name string) error {
	fo, err := os.Create(name)
	if err != nil {
		return err
	}
	defer func() {
		fo.Close()
	}()
	return nil
}

var log *eventlog.Log

func main() {
	var err error
	logfileName := "D:/auto-push.log"
	log, err = eventlog.Open("git-auto-push")
	if err != nil {
		panic(fmt.Sprintf("error: %v", err))
	}
	if !FileExists(logfileName) {
		CreateFile(logfileName)
	}

	// attempt #1
	prg := &program{}

	// Call svc.Run to start your program/service.
	if err := svc.Run(prg); err != nil {
		log.Error(100, err.Error())
		panic("svc.Run error")
	}
}

func (p *program) Init(env svc.Environment) error {
	log.Info(200, spt("is win service? %v\n", env.IsWindowsService()))
	return nil
}

func (p *program) Start() error {
	// The Start method must not block, or Windows may assume your service failed
	// to start. Launch a Goroutine here to do something interesting/blocking.

	p.quit = make(chan struct{})

	p.wg.Add(1)
	go start()
	go func() {
		log.Info(200, "Starting...")
		<-p.quit
		log.Info(200, "Quit signal received...")
		log.Close()
		p.wg.Done()
	}()

	log.Info(200, "Not block!")
	return nil
}

func (p *program) Stop() error {
	// The Stop method is invoked by stopping the Windows service, or by pressing Ctrl+C on the console.
	// This method may block, but it's a good idea to finish quickly or your process may be killed by
	// Windows during a shutdown/reboot. As a general rule you shouldn't rely on graceful shutdown.

	log.Info(200, "Stopping...")
	close(p.quit)
	p.wg.Wait()
	log.Info(200, "Stopped.")
	return nil
}

var spt = fmt.Sprintf

func start() {
	f, err := os.OpenFile("D:/auto-config.json", os.O_RDONLY, 0766)
	if err != nil {
		log.Error(100, fmt.Sprintf("failed to open config file, err: %+v\n", err))
		panic("error")
	}
	defer f.Close()

	bs, err := io.ReadAll(f)
	if err != nil {
		log.Error(100, spt("failed to read config file, err: %+v\n", err))
		panic("error")
	}

	var c Config
	err = json.Unmarshal(bs, &c)
	if err != nil {
		log.Error(100, spt("failed to decode config file, err: %+v\n", err))
		panic("error")
	}
	itvl, repos := c.Interval, c.Repositories
	if itvl == 0 {
		itvl = 10
	}
	for {
		autoPush(repos, c.IsCommit)
		time.Sleep(time.Duration(itvl) * time.Second)
	}
}

func autoCommit(repo Repository) (isContinue bool) {
	cmd := exec.Command("git", "add", ".")
	bs, err := cmd.Output()
	if err != nil {
		log.Info(101, spt("ERROR: failed to run 'git add', err: %+v, path: %s, output: %s\n", err, repo.Path, string(bs)))
		return true
	}
	curTime := time.Now().Format("2006/01/02 15:04:05")
	cmd = exec.Command("git", "commit", "-m", fmt.Sprintf("%s auto commit", curTime))
	bs, err = cmd.Output()
	if err != nil {
		log.Info(101, spt("ERROR: failed to run 'git commit', err: %+v, path: %s, output: %s\n", err, repo.Path, string(bs)))
		return true
	}
	return false
}

func autoPush(repos []Repository, isCommit bool) {
	ex, err := os.Executable()
	if err != nil {
		log.Error(100, spt("failed to find current working dir, err: %+v\n", err))
		panic("stop")
	}
	oriDir := filepath.Dir(ex)
	var success []string
	for i := 0; i < len(repos); i++ {
		repo := repos[i]
		p := repo.Path
		if len(p) == 0 {
			log.Info(200, "WARNING: encounter an empty path")
			continue
		}

		s, err := os.Stat(p)
		if err != nil {
			log.Error(101, spt("ERROR: failed to get directory stat, err: %+v, path: %s\n", err, repo.Path))
			continue
		}
		if !s.IsDir() {
			log.Error(101, spt("ERROR: %s is not a directory\n", repo.Path))
			continue
		}
		os.Chdir(p)

		if err != nil {
			log.Error(101, spt("ERROR: failed to change working dir, err: %+v, path: %s\n", err, repo.Path))
			continue
		}

		if isCommit && autoCommit(repo) {
			continue
		}

		cmd := exec.Command("git", "push", repo.Remote, repo.Branch)
		bs, err := cmd.Output()
		if err != nil {
			log.Error(101, spt("ERROR: failed to run 'git push', err: %+v, path: %s, output: %s\n", err, repo.Path, string(bs)))
			continue
		}
		success = append(success, repo.Path)
	}
	os.Chdir(oriDir)
	if len(success) == 0 {
		log.Info(200, "No repository pushed")
		return
	}
	s := strings.Join(success, "\n")
	log.Info(200, spt("Successfully pushed repositories: %s\n", s))
}
