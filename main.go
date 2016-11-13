package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"archive/zip"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/zeropage/mukgoorm/cmd"
	"github.com/zeropage/mukgoorm/grant"
	"github.com/zeropage/mukgoorm/setting"
)

const SESSION_EXPIRE_TIME int = 1800

type FilePathInfo struct {
	File os.FileInfo
	Path string
}

func getFileInfoAndPath(root string) (*[]FilePathInfo, error) {
	files := []FilePathInfo{}
	err := filepath.Walk(root, filepath.WalkFunc(func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip base directory
		if info.Name() == filepath.Base(root) {
			return nil
		}

		files = append(files, FilePathInfo{info, path})
		if info.IsDir() {
			return filepath.SkipDir
		}
		return err
	}))
	return &files, err
}

func makeZip(foldername string) (string, error) {
	newfile, err := os.Create(foldername + ".zip")
  if err != nil {
		return "", err
	}
  defer newfile.Close()

  zipit := zip.NewWriter(newfile)
  defer zipit.Close()

  filepath.Walk(foldername, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

    if info.Name() == filepath.Base(foldername) {
			return nil
		}

    header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
    header.Name = strings.TrimPrefix(path, foldername)

    if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}
    writer, err := zipit.CreateHeader(header)
    if err != nil {
			return err
		}

    if info.IsDir() {
			return nil
		}

    zipfile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer zipfile.Close()

		_, err = io.Copy(writer, zipfile)
		return err

	})
  return foldername + ".zip", err
 }

func checkLogin(c *gin.Context) {
	session := sessions.Default(c)
	auth := grant.FromSession(session.Get("authority"))

	authorized, err := grant.AuthorityExist(auth)
	if !authorized {
		c.Redirect(http.StatusSeeOther, "/login")
		c.Abort()
	}
	if err != nil {
		panic(err)
		c.Redirect(http.StatusSeeOther, "/login")
		c.Abort()
	}
}

func checkAuthority(c *gin.Context) {
	checkLogin(c)

	session := sessions.Default(c)
	auth := grant.FromSession(session.Get("authority"))

	switch auth {
	case grant.ADMIN:
		session.Options(sessions.Options{MaxAge: SESSION_EXPIRE_TIME})
	case grant.READ_ONLY:
		session.Options(sessions.Options{MaxAge: SESSION_EXPIRE_TIME})
		c.Redirect(http.StatusSeeOther, "/list")
	}
}

// When starting server directory parameter is needed. Else error occurs.
// Run Command:
//	go run main.go -D tmp/dat -A *PASSWORD* -R *PASSWORD*
func main() {
	cmd.RootCmd.Execute()
	r := NewEngine()
	// FIXME recieve hostname or bind address

	r.Run("localhost:8080")
}

func NewEngine() *gin.Engine {
	r := gin.Default()

	r.Static("/list", "./templates/javascript")
	r.LoadHTMLGlob("templates/*/*.tmpl")

	shareDir := setting.GetDirectory()
	sharePassword := setting.GetPassword()

	store := sessions.NewCookieStore([]byte("secret"))
	r.Use(sessions.Sessions("_sess", store))

	r.GET("/login", func(c *gin.Context) {
		c.HTML(http.StatusOK, "input_password.tmpl", gin.H{})
	})

	r.POST("/login", func(c *gin.Context) {
		password := c.PostForm("password")

		authority := grant.FromPassword(password)
		session := sessions.Default(c)
		// INFO: if you just put authority which is Grant type, then session save nil....
		session.Set("authority", int(authority))
		session.Options(sessions.Options{MaxAge: SESSION_EXPIRE_TIME})
		session.Save()

		c.Redirect(http.StatusFound, "/list")
	})

	r.GET("/set-password", checkAuthority, func(c *gin.Context) {
		c.HTML(http.StatusOK, "set_password.tmpl", gin.H{})
	})

	r.POST("/set-password", func(c *gin.Context) {
		sharePassword.AdminPassword = c.PostForm("adminPassword")
		sharePassword.ReadOnlyPassword = c.PostForm("readOnlyPassword")

		c.Redirect(http.StatusSeeOther, "/login")
	})

	r.GET("/list", checkLogin, func(c *gin.Context) {

		sharedPath := c.Query("dir")
		if sharedPath == "" {
			sharedPath = shareDir.Path
		} else if !shareDir.ValidDir(sharedPath) {
			c.HTML(http.StatusNotFound, "404.tmpl", gin.H{})
		}

		files, err := getFileInfoAndPath(sharedPath)
		if err != nil {
			c.HTML(http.StatusNotFound, "404.tmpl", gin.H{})
		}
		c.HTML(http.StatusOK, "list.tmpl", gin.H{
			"files": files,
		})
	})

	r.GET("/down", func(c *gin.Context) {
		checkAuthority(c)
		fileName := c.Query("dir")
		file, err := os.OpenFile(fileName, os.O_RDONLY, 0440)
		fileinfo, err := file.Stat()
		if fileinfo.IsDir() {
			fileName, err = makeZip(fileName)
			if err != nil {
				panic(err)
			}
			defer os.Remove(fileName)
		}
		filedata, err := ioutil.ReadFile(fileName)
		if err != nil {
			c.HTML(http.StatusNotFound, "errors/404.tmpl", gin.H{})
			log.Info(err)
		}

		c.Data(http.StatusOK, "application/octet-stream", filedata)
	})

	r.GET("/info", func(c *gin.Context) {
		fileName := c.Query("dir")
		file, err := os.OpenFile(fileName, os.O_RDONLY, 0440)
		if err != nil {
			panic(err)
		}

		c.HTML(http.StatusOK, "info.tmpl", gin.H{
			"file": file,
		})
		// this code is just give url(ex. localhost:8080/list?fn=hello2.txt)
	})

	r.POST("/upload", func(c *gin.Context) {

		file, header, err := c.Request.FormFile("image")
		if err != nil {
			panic(err)
		}
		filename := header.Filename
		fmt.Println(header.Filename)
		out, err := os.Create("./tmp/dat/" + filename)
		if err != nil {
			log.Fatal(err)
		}
		defer out.Close()
		_, err = io.Copy(out, file)
		if err != nil {
			log.Fatal(err)
		}
		c.Redirect(http.StatusMovedPermanently, "http://localhost:8080/list")

	})
	return r
}
