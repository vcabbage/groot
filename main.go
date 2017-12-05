package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	os.Exit(run())
}

const binaryRelease = "1.9.2"

var distToHash = map[string]string{
	"android/386":     "",
	"android/amd64":   "",
	"android/arm":     "",
	"android/arm64":   "",
	"darwin/386":      "",
	"darwin/amd64":    "73fd5840d55f5566d8db6c0ffdd187577e8ebe650c783f68bd27cbf95bde6743",
	"darwin/arm":      "",
	"darwin/arm64":    "",
	"dragonfly/amd64": "",
	"freebsd/386":     "809dcb0a8457c8d0abf954f20311a1ee353486d0ae3f921e9478189721d37677",
	"freebsd/amd64":   "8be985c3e251c8e007fa6ecd0189bc53e65cc519f4464ddf19fa11f7ed251134",
	"freebsd/arm":     "",
	"linux/386":       "574b2c4b1a248e58ef7d1f825beda15429610a2316d9cbd3096d8d3fa8c0bc1a",
	"linux/amd64":     "de874549d9a8d8d8062be05808509c09a88a248e77ec14eb77453530829ac02b",
	"linux/arm":       "",
	"linux/arm64":     "0016ac65ad8340c84f51bc11dbb24ee8265b0a4597dbfdf8d91776fc187456fa",
	"linux/mips":      "",
	"linux/mips64":    "",
	"linux/mips64le":  "",
	"linux/mipsle":    "",
	"linux/ppc64":     "",
	"linux/ppc64le":   "adb440b2b6ae9e448c253a20836d8e8aa4236f731d87717d9c7b241998dc7f9d",
	"linux/s390x":     "a7137b4fbdec126823a12a4b696eeee2f04ec616e9fb8a54654c51d5884c1345",
	"nacl/386":        "",
	"nacl/amd64p32":   "",
	"nacl/arm":        "",
	"netbsd/386":      "",
	"netbsd/amd64":    "",
	"netbsd/arm":      "",
	"openbsd/386":     "",
	"openbsd/amd64":   "",
	"openbsd/arm":     "",
	"plan9/386":       "",
	"plan9/amd64":     "",
	"plan9/arm":       "",
	"solaris/amd64":   "",
	"windows/386":     "",
	"windows/amd64":   "",
}

var commands = map[string]func(_ groot, args ...string) int{
	"activate":  activate,
	"add":       add,
	"available": available,
	"env":       env,
	"init":      initGroot,
	"list":      list,
}

func run() int {
	log.SetFlags(log.Lshortfile)

	if len(os.Args) < 2 {
		fmt.Println(`groot: GOROOT manager`)
		return 0
	}

	cmd, ok := commands[os.Args[1]]
	if !ok {
		fmt.Println("unknown subcommand:", os.Args[1])
		return 1
	}

	// Find Home Directory
	user, err := user.Current()
	if err != nil {
		return printError(err)
	}

	if user.HomeDir == "" {
		fmt.Println("Unable to determine user's home directory.")
		return 1
	}

	baseDir := filepath.Join(user.HomeDir, ".groot")
	g := groot{
		baseDir:   baseDir,
		gitDir:    filepath.Join(baseDir, ".bare"),
		binaryDir: filepath.Join(baseDir, ".binary"),
	}

	return cmd(g, os.Args[2:]...)
}

type groot struct {
	baseDir   string
	gitDir    string
	binaryDir string
	verbose   bool
}

func (g *groot) init() error {
	// Create .groot
	err := os.MkdirAll(g.baseDir, 0700)
	if err != nil {
		return err
	}

	// Download binary release
	err = downloadBinaryRelease(g.binaryDir)
	if err != nil {
		return err
	}

	// Clone bare repo
	err = g.exec("git", "clone", "--bare", "https://go.googlesource.com/go", g.gitDir)
	if err != nil {
		return err
	}

	// https://stackoverflow.com/questions/39882988/git-bare-repo-cannot-have-a-worktree-for-master-branch-why
	err = g.git("update-ref", "--no-deref", "HEAD", "HEAD^{commit}")
	if err != nil {
		return err
	}

	// Create worktrees
	tags := []string{"go1.7", "go1.9"} // TODO: install latest
	for _, tag := range tags {
		g.branchAndBuild(tag)
	}

	activeBranch := filepath.Join(g.baseDir, tags[len(tags)-1], "bin")
	activePath := filepath.Join(g.baseDir, "bin")
	return os.Symlink(activeBranch, activePath)
}

func (g *groot) activate(tag string) error {
	bin := filepath.Join(g.baseDir, tag, "bin")

	_, err := os.Stat(bin)
	if err != nil {
		return err
	}

	activePath := filepath.Join(g.baseDir, "bin")
	err = os.Remove(activePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	return os.Symlink(bin, activePath)
}

func (g *groot) git(args ...string) error {
	return g.exec("git", append([]string{"--git-dir", g.gitDir}, args...)...)
}

func (g *groot) exec(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if g.verbose {
		fmt.Println("Running:", name, strings.Join(args, " "))
	}

	return cmd.Run()
}

func (g *groot) branchAndBuild(tag string) error {
	_, err := os.Stat(filepath.Join(g.baseDir, tag))
	if !os.IsNotExist(err) {
		return err
	}

	branch := "groot." + tag

	err = g.git("branch", branch, tag)
	if err != nil {
		return err
	}

	worktreePath := filepath.Join(g.baseDir, tag)
	err = g.git("worktree", "add", worktreePath, branch)
	if err != nil {
		return err
	}

	cmd := exec.Command("./make.bash")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = filepath.Join(worktreePath, "src")
	cmd.Env = append(os.Environ(), "GOROOT_BOOTSTRAP="+g.binaryDir)
	return cmd.Run()
}

func (g *groot) list() error {
	return g.git("worktree", "list")
}

func env(g groot, args ...string) int {
	fmt.Printf("export PATH=\"$PATH:%s\"\n", filepath.Join(g.baseDir, "bin"))

	finfos, err := ioutil.ReadDir(g.baseDir)
	if err != nil {
		return printError(err)
	}

	for _, finfo := range finfos {
		name := finfo.Name()
		if name == "bin" || strings.HasPrefix(name, ".") {
			continue
		}

		fmt.Printf("alias %s=%s\n", name, filepath.Join(g.baseDir, name, "bin/go"))
	}

	return 0
}

func add(g groot, args ...string) int {
	if len(args) < 1 {
		fmt.Println(os.Args[0], "add [tag]")
		return 1
	}
	tag := args[0]

	err := g.branchAndBuild(tag)
	if err != nil {
		return printError(err)
	}
	return 0
}

func list(g groot, _ ...string) int {
	err := g.git("worktree", "list")
	if err != nil {
		return printError(err)
	}
	return 0
}

func activate(g groot, args ...string) int {
	if len(args) < 1 {
		fmt.Println(os.Args[0], "activate [tag]")
		return 1
	}
	tag := args[0]

	err := g.activate(tag)
	if err != nil {
		return printError(err)
	}
	fmt.Println(tag, "activated!")
	return 0
}

func available(g groot, _ ...string) int {
	err := g.git("tag", "--list", "go*")
	if err != nil {
		return printError(err)
	}
	return 0
}

func initGroot(g groot, _ ...string) int {
	err := g.init()
	if err != nil {
		return printError(err)
	}
	return 0
}

func printError(err error) int {
	log.Println("Error:", err)
	return 1
}

func downloadBinaryRelease(dir string) error {
	dist := runtime.GOOS + "/" + runtime.GOARCH
	hash, ok := distToHash[dist]
	if !ok {
		return fmt.Errorf("Unknown OS/Architecture: %s", dist)
	}
	if hash == "" {
		return fmt.Errorf("Unsupported OS/Architecture: %s", dist)
	}

	url := fmt.Sprintf("https://redirector.gvt1.com/edgedl/go/go%s.%s-%s.tar.gz", binaryRelease, runtime.GOOS, runtime.GOARCH)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Println("Downloading binary release: unexpected status:", resp.Status)
		if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "text/plain") {
			body, err := ioutil.ReadAll(resp.Body)
			if err == nil {
				log.Println(string(body))
			}
		}
		return errors.New("")
	}

	hasher := sha256.New()

	err = extractTarGz(io.TeeReader(resp.Body, hasher), dir)
	if err != nil {
		return err
	}

	if got := hex.EncodeToString(hasher.Sum(nil)); got != hash {
		log.Println("Downloaded binary release does not match published SHA256 hash.")
		fmt.Println(hash)
		fmt.Println(got)
		return errors.New("")
	}

	return nil
}

func extractTarGz(r io.Reader, dir string) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gr)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		name := filepath.Join(dir, strings.TrimPrefix(hdr.Name, "go"))

		switch hdr.Typeflag {
		case tar.TypeDir:
			fmt.Printf("Directory: %s\n", name)
			err := os.MkdirAll(name, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
		case tar.TypeReg:
			fmt.Printf("File: %s\n", name)
			f, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tr)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("Unexpected type %c\n", hdr.Typeflag)
		}
	}

	return nil
}
