// Configurations manipulations, all configurations (including user configurations) are stored in wide.json.
package conf

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/b3log/wide/event"
	"github.com/b3log/wide/util"
	"github.com/golang/glog"
)

const (
	PathSeparator     = string(os.PathSeparator)     // OS-specific path separator
	PathListSeparator = string(os.PathListSeparator) // OS-specific path list separator
)

const (
	WideVersion   = "1.0.1" // wide version
	CodeMirrorVer = "4.7"   // editor version
)

// The latest session content.
type LatestSessionContent struct {
	FileTree    []string // paths of expanding nodes of file tree
	Files       []string // paths of files of opening editor tabs
	CurrentFile string   // path of file of the current focused editor tab
}

// User configuration.
type User struct {
	Name                 string
	Password             string
	Workspace            string // the GOPATH of this user
	Locale               string
	GoFormat             string
	FontFamily           string
	FontSize             string
	Editor               *Editor
	LatestSessionContent *LatestSessionContent
}

// Editor configuration of a user.
type Editor struct {
	FontFamily string
	FontSize   string
}

// Configuration.
type conf struct {
	IP                    string  // server ip, ${ip}
	Server                string  // server host and port ({IP}:7070)
	StaticServer          string  // static resources server scheme, host and port (http://{IP}:7070)
	EditorChannel         string  // editor channel (ws://{IP}:7070)
	OutputChannel         string  // output channel (ws://{IP}:7070)
	ShellChannel          string  // shell channel(ws://{IP}:7070)
	SessionChannel        string  // wide session channel (ws://{IP}:7070)
	HTTPSessionMaxAge     int     // HTTP session max age (in seciond)
	StaticResourceVersion string  // version of static resources
	MaxProcs              int     // Go max procs
	RuntimeMode           string  // runtime mode (dev/prod)
	WD                    string  // current working direcitory, ${pwd}
	Workspace             string  // path of master workspace
	Locale                string  // default locale
	Users                 []*User // configurations of users
}

// Configuration variable.
var Wide conf

// A raw copy of configuration variable.
//
// Save function will use this variable to persist.
var rawWide conf

// FixedTimeCheckEnv checks Wide runtime enviorment periodically (7 minutes).
//
// Exits process if found fatal issues (such as not found $GOPATH),
// Notifies user by notification queue if found warning issues (such as not found gocode).
func FixedTimeCheckEnv() {
	checkEnv() // check immediately

	go func() {
		for _ = range time.Tick(time.Minute * 7) {
			checkEnv()
		}
	}()
}

func checkEnv() {
	if "" == os.Getenv("GOPATH") {
		glog.Fatal("Not found $GOPATH, please configure it before running Wide")

		os.Exit(-1)
	}

	if "" == os.Getenv("GOROOT") {
		glog.Fatal("Not found $GOROOT, please configure it before running Wide")

		os.Exit(-1)
	}

	gocode := Wide.GetExecutableInGOBIN("gocode")
	cmd := exec.Command(gocode, "close")
	_, err := cmd.Output()
	if nil != err {
		event.EventQueue <- &event.Event{Code: event.EvtCodeGocodeNotFound}

		glog.Warningf("Not found gocode [%s]", gocode)
	}

	ide_stub := Wide.GetExecutableInGOBIN("ide_stub")
	cmd = exec.Command(ide_stub, "version")
	_, err = cmd.Output()
	if nil != err {
		event.EventQueue <- &event.Event{Code: event.EvtCodeIDEStubNotFound}

		glog.Warningf("Not found ide_stub [%s]", ide_stub)
	}
}

// FixedTimeSave saves configurations (wide.json) periodically (1 minute).
//
// Main goal of this function is to save user session content, for restoring session content while user open Wide next time.
func FixedTimeSave() {
	go func() {
		for _ = range time.Tick(time.Minute) {
			Save()
		}
	}()
}

// GetUserWorkspace gets workspace path with the specified username, returns "" if not found.
func (c *conf) GetUserWorkspace(username string) string {
	for _, user := range c.Users {
		if user.Name == username {
			ret := strings.Replace(user.Workspace, "{WD}", c.WD, 1)

			return filepath.FromSlash(ret)
		}
	}

	return ""
}

// GetWorkspace gets the master workspace path.
//
// Compared to the use of Wide.Workspace, this function will be processed as follows:
//  1. Replace {WD} variable with the actual directory path
//  2. Replace "/" with "\\" (Windows)
func (c *conf) GetWorkspace() string {
	return filepath.FromSlash(strings.Replace(c.Workspace, "{WD}", c.WD, 1))
}

// GetGoFmt gets the path of Go format tool, returns "gofmt" if not found.
func (c *conf) GetGoFmt(username string) string {
	for _, user := range c.Users {
		if user.Name == username {
			switch user.GoFormat {
			case "gofmt":
				return "gofmt"
			case "goimports":
				return c.GetExecutableInGOBIN("goimports")
			default:
				glog.Errorf("Unsupported Go Format tool [%s]", user.GoFormat)
				return "gofmt"
			}
		}
	}

	return "gofmt"
}

// GetWorkspace gets workspace path of the user.
//
// Compared to the use of Wide.Workspace, this function will be processed as follows:
//  1. Replace {WD} variable with the actual directory path
//  2. Replace "/" with "\\" (Windows)
func (u *User) GetWorkspace() string {
	return filepath.FromSlash(strings.Replace(u.Workspace, "{WD}", Wide.WD, 1))
}

// GetUser gets configuration of the user specified by the given username, returns nil if not found.
func (*conf) GetUser(username string) *User {
	for _, user := range Wide.Users {
		if user.Name == username {
			return user
		}
	}

	return nil
}

// GetExecutableInGOBIN gets executable file under GOBIN path.
//
// The specified executable should not with extension, this function will append .exe if on Windows.
func (*conf) GetExecutableInGOBIN(executable string) string {
	if util.OS.IsWindows() {
		executable += ".exe"
	}

	gopaths := filepath.SplitList(os.Getenv("GOPATH"))

	for _, gopath := range gopaths {
		// $GOPATH/bin/$GOOS_$GOARCH/executable
		ret := gopath + PathSeparator + "bin" + PathSeparator +
			os.Getenv("GOOS") + "_" + os.Getenv("GOARCH") + PathSeparator + executable
		if isExist(ret) {
			return ret
		}

		// $GOPATH/bin/{runtime.GOOS}_{runtime.GOARCH}/executable
		ret = gopath + PathSeparator + "bin" + PathSeparator +
			runtime.GOOS + "_" + runtime.GOARCH + PathSeparator + executable
		if isExist(ret) {
			return ret
		}

		// $GOPATH/bin/executable
		ret = gopath + PathSeparator + "bin" + PathSeparator + executable
		if isExist(ret) {
			return ret
		}
	}

	// $GOBIN/executable
	return os.Getenv("GOBIN") + PathSeparator + executable
}

// Save saves Wide configurations.
func Save() bool {
	// just the Users field are volatile
	rawWide.Users = Wide.Users

	// format
	bytes, err := json.MarshalIndent(rawWide, "", "    ")

	if nil != err {
		glog.Error(err)

		return false
	}

	if err = ioutil.WriteFile("conf/wide.json", bytes, 0644); nil != err {
		glog.Error(err)

		return false
	}

	return true
}

// Load loads the configurations from wide.json.
func Load() {
	bytes, _ := ioutil.ReadFile("conf/wide.json")

	err := json.Unmarshal(bytes, &Wide)
	if err != nil {
		glog.Error(err)

		os.Exit(-1)
	}

	// keep the raw content
	json.Unmarshal(bytes, &rawWide)

	ip, err := util.Net.LocalIP()
	if err != nil {
		glog.Error(err)

		os.Exit(-1)
	}

	glog.V(5).Infof("${ip} [%s]", ip)

	Wide.WD = util.OS.Pwd()
	glog.V(5).Infof("${pwd} [%s]", Wide.WD)

	Wide.IP = strings.Replace(Wide.IP, "${ip}", ip, 1)

	Wide.Server = strings.Replace(Wide.Server, "{IP}", Wide.IP, 1)
	Wide.StaticServer = strings.Replace(Wide.StaticServer, "{IP}", Wide.IP, 1)
	Wide.EditorChannel = strings.Replace(Wide.EditorChannel, "{IP}", Wide.IP, 1)
	Wide.OutputChannel = strings.Replace(Wide.OutputChannel, "{IP}", Wide.IP, 1)
	Wide.ShellChannel = strings.Replace(Wide.ShellChannel, "{IP}", Wide.IP, 1)
	Wide.SessionChannel = strings.Replace(Wide.SessionChannel, "{IP}", Wide.IP, 1)

	glog.V(5).Info("Conf: \n" + string(bytes))

	initWorkspaceDirs()
	initCustomizedConfs()
}

// initCustomizedConfs initializes the user customized configurations.
func initCustomizedConfs() {
	for _, user := range Wide.Users {
		UpdateCustomizedConf(user.Name)
	}
}

// UpdateCustomizedConf creates (if not exists) or updates user customized configuration files.
//
//  1. /static/user/{username}/style.css
func UpdateCustomizedConf(username string) {
	var u *User = nil
	for _, user := range Wide.Users { // maybe it is a beauty of the trade-off of the another world between design and implementation
		if user.Name == username {
			u = user
		}
	}

	if nil == u {
		return
	}

	model := map[string]interface{}{"user": u}

	t, err := template.ParseFiles("static/user/style.css.tmpl")
	if nil != err {
		glog.Error(err)

		os.Exit(-1)
	}

	wd := util.OS.Pwd()
	dir := filepath.Clean(wd + "/static/user/" + u.Name)
	if err := os.MkdirAll(dir, 0755); nil != err {
		glog.Error(err)

		os.Exit(-1)
	}

	fout, err := os.Create(dir + PathSeparator + "style.css")
	if nil != err {
		glog.Error(err)

		os.Exit(-1)
	}

	defer fout.Close()

	if err := t.Execute(fout, model); nil != err {
		glog.Error(err)

		os.Exit(-1)
	}
}

// initWorkspaceDirs initializes the directories of master workspace, users' workspaces.
//
// Creates directories if not found on path of workspace.
func initWorkspaceDirs() {
	paths := filepath.SplitList(Wide.GetWorkspace())

	for _, user := range Wide.Users {
		paths = append(paths, filepath.SplitList(user.GetWorkspace())...)
	}

	for _, path := range paths {
		CreateWorkspaceDir(path)
	}
}

// CreateWorkspaceDir creates (if not exists) directories on the path.
//
//  1. root directory:{path}
//  2. src directory: {path}/src
//  3. package directory: {path}/pkg
//  4. binary directory: {path}/bin
func CreateWorkspaceDir(path string) {
	createDir(path)
	createDir(path + PathSeparator + "src")
	createDir(path + PathSeparator + "pkg")
	createDir(path + PathSeparator + "bin")
}

// createDir creates a directory on the path if it not exists.
func createDir(path string) {
	if !isExist(path) {
		if err := os.MkdirAll(path, 0775); nil != err {
			glog.Error(err)

			os.Exit(-1)
		}

		glog.V(7).Infof("Created a directory [%s]", path)
	}
}

// isExist determines whether the file spcified by the given filename is exists.
func isExist(filename string) bool {
	_, err := os.Stat(filename)

	return err == nil || os.IsExist(err)
}
