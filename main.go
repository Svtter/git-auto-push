package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/judwhite/go-svc"
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

func main() {
	logfileName := "D:/auto-push.log"
	if !FileExists(logfileName) {
		CreateFile(logfileName)
	}
	f, err := os.OpenFile(logfileName, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()
	if err != nil {
		log.Fatalf("error: %v", err)
	}

	// attempt #1
	log.SetOutput(f)
	prg := &program{}

	// Call svc.Run to start your program/service.
	if err := svc.Run(prg); err != nil {
		log.Fatal(err)
	}
}

func (p *program) Init(env svc.Environment) error {
	log.Printf("is win service? %v\n", env.IsWindowsService())
	return nil
}

func (p *program) Start() error {
	// The Start method must not block, or Windows may assume your service failed
	// to start. Launch a Goroutine here to do something interesting/blocking.

	p.quit = make(chan struct{})

	p.wg.Add(1)
	go start()
	go func() {
		log.Println("Starting...")
		<-p.quit
		log.Println("Quit signal received...")
		p.wg.Done()
	}()

	log.Println("Not block!")
	return nil
}

func (p *program) Stop() error {
	// The Stop method is invoked by stopping the Windows service, or by pressing Ctrl+C on the console.
	// This method may block, but it's a good idea to finish quickly or your process may be killed by
	// Windows during a shutdown/reboot. As a general rule you shouldn't rely on graceful shutdown.

	log.Println("Stopping...")
	close(p.quit)
	p.wg.Wait()
	log.Println("Stopped.")
	return nil
}

func start() {
	f, err := os.OpenFile("D:/auto-config.json", os.O_RDONLY, 0766)
	if err != nil {
		log.Fatalf("failed to open config file, err: %+v\n", err)
	}
	defer f.Close()

	bs, err := io.ReadAll(f)
	if err != nil {
		log.Fatalf("failed to read config file, err: %+v\n", err)
	}

	var c Config
	err = json.Unmarshal(bs, &c)
	if err != nil {
		log.Fatalf("failed to decode config file, err: %+v\n", err)
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
		log.Printf("ERROR: failed to run 'git add', err: %+v, path: %s, output: %s\n", err, repo.Path, string(bs))
		return true
	}
	curTime := time.Now().Format("2006/01/02 15:04:05")
	cmd = exec.Command("git", "commit", "-m", fmt.Sprintf("%s auto commit", curTime))
	bs, err = cmd.Output()
	if err != nil {
		log.Printf("ERROR: failed to run 'git commit', err: %+v, path: %s, output: %s\n", err, repo.Path, string(bs))
		return true
	}
	return false
}

func autoPush(repos []Repository, isCommit bool) {
	ex, err := os.Executable()
	if err != nil {
		log.Fatalf("failed to find current working dir, err: %+v\n", err)
	}
	oriDir := filepath.Dir(ex)
	var success []string
	for i := 0; i < len(repos); i++ {
		repo := repos[i]
		p := repo.Path
		if len(p) == 0 {
			log.Println("WARNING: encounter an empty path")
			continue
		}

		s, err := os.Stat(p)
		if err != nil {
			log.Printf("ERROR: failed to get directory stat, err: %+v, path: %s\n", err, repo.Path)
			continue
		}
		if !s.IsDir() {
			log.Printf("ERROR: %s is not a directory\n", repo.Path)
			continue
		}
		os.Chdir(p)

		if err != nil {
			log.Printf("ERROR: failed to change working dir, err: %+v, path: %s\n", err, repo.Path)
			continue
		}

		if isCommit && autoCommit(repo) {
			continue
		}

		cmd := exec.Command("git", "push", repo.Remote, repo.Branch)
		bs, err := cmd.Output()
		if err != nil {
			log.Printf("ERROR: failed to run 'git push', err: %+v, path: %s, output: %s\n", err, repo.Path, string(bs))
			continue
		}
		success = append(success, repo.Path)
	}
	os.Chdir(oriDir)
	if len(success) == 0 {
		fmt.Println("No repository pushed")
		return
	}
	s := strings.Join(success, "\n")
	fmt.Printf("Successfully pushed repositories: %s\n", s)
}
