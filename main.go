package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

const (
	envBaseApiUrl    = "BASE_API_URL"
	envBaseDir       = "BASE_DIR"
	envIgnoreFile    = "IGNORE_FILE"
	envSyncToken     = "TOKEN"
	envBaseNamespace = "BASE_NAMESPACE"
	version          = "1.0.0"
	usage            = `
雨雀文档同步脚本：
  命令：
    version         输出版本信息
    sync            执行同步脚本
        -default    默认命令
    rebuild         重新构建book.json
    help            输出当前信息
  配置：
    BASE_API_URL    雨雀基础路由地址
    BASE_DIR        同步脚本启动目录
    IGNORE_FILE     忽略文件文件地址
    TOKEN           雨雀TOKEN
    BASE_NAMESPACE  同步文档命名空间
`
)

var (
	baseApiUrl     = "https://www.yuque.com/api/v2"
	baseDir        = ""
	ignoreFile     = ".ignoresync"
	ignoreFilePath = ""
	token          = ""
	allowedSuffix  = []string{".md"}
	baseNamespace  = "tests/sync"
)

func init() {
	if os.Getenv(envBaseApiUrl) != "" {
		baseApiUrl = os.Getenv(envBaseApiUrl)
	}

	if os.Getenv(envBaseDir) == "" {
		baseDir, _ = os.Getwd()
	} else {
		baseDir = os.Getenv(envBaseDir)
	}

	if os.Getenv(envIgnoreFile) != "" {
		ignoreFile = os.Getenv(envIgnoreFile)
	}

	if os.Getenv(envSyncToken) != "" {
		token = os.Getenv(envSyncToken)
	}

	if os.Getenv(envBaseNamespace) != "" {
		baseNamespace = os.Getenv(envBaseNamespace)
	}

	ignoreFilePath = path.Join(baseDir, ignoreFile)
}

func main() {
	ignores := readIgnore()

	command := "sync"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	books := new(Books)
	books.Parse(path.Join(baseDir, "book.json"))
	scanFiles(ignores, ".", buildBook(books))

	switch command {
	case "sync":

		y := NewYuque(baseApiUrl)
		books.ReadEach(saveYuQue(y))
		_ = books.Save()
		break
	case "rebuild":
		if err := books.Save(); err != nil {
			log.Print("数据文件生成失败")
			os.Exit(0)
		}
		break
	case "version":
		log.Printf("雨雀同步文档版本：%s", version)
		break
	case "help":
		log.Print(usage)
		break
	default:
		log.Println("warning： 未知命令")
		log.Print(usage)
		break
	}
	os.Exit(0)
}

func buildBook(books *Books) func(info os.FileInfo, dir string) {
	now := time.Now()
	return func(info os.FileInfo, dir string) {
		var (
			book     *Book
			filepath = path.Join(dir, info.Name())
			slug     = encodeString(filepath)
		)

		has, idx, book := books.FindBySlug(slug)
		if !has {
			book = new(Book)
		}

		book.Title = ""
		book.Version = "1.0.0"
		book.UpdatedAt = &now
		book.Name = info.Name()
		book.Slug = slug
		book.Dir = dir
		book.Path = path.Join(filepath)
		if idx == 0 {
			books.AppendBook(*book)
		}
	}
}

func readIgnore() []string {
	body, err := ioutil.ReadFile(ignoreFilePath)
	if err != nil {
		return nil
	}

	ignores := strings.Split(string(body[:]), "\n")
	ignores = append(ignores, ignoreFile)
	return ignores
}

func scanFiles(ignores []string, parent string, fileHandle func(info os.FileInfo, dir string)) {
	files, _ := ioutil.ReadDir(parent)
	for _, file := range files {
		if !isIgnores(ignores, file.Name(), parent) {
			if file.IsDir() {
				scanFiles(ignores, path.Join(parent, file.Name()), fileHandle)
			} else if isSupportSuffix(file.Name()) {
				fileHandle(file, parent)
			}

		}
	}
}

func isIgnores(ignores []string, name string, currentPath string) bool {
	for _, ignore := range ignores {
		if strings.Contains(ignore, "/") {
			if path.Join(baseDir, ignore) == path.Join(currentPath, name) {
				return true
			}
		} else if name == ignore {
			return true
		}
	}
	return false
}

func isSupportSuffix(filename string) bool {
	for _, suffix := range allowedSuffix {
		if !strings.HasSuffix(filename, suffix) {
			return false
		}
	}
	return true
}

type Yuque struct {
	baseUri string
	client  *http.Client
}

func NewYuque(baseUri string) *Yuque {
	return &Yuque{
		baseUri: baseUri,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (y *Yuque) Save(body []byte, b *Book) error {
	type Resp struct {
		Data DocDetailSerializer `json:"data"`
	}
	resp := new(Resp)
	if b.Id != 0 {
		if err := y.Put(fmt.Sprintf("/repos/%s/docs/%d", baseNamespace, b.Id), body, resp); err != nil {
			return err
		}
	} else {
		if err := y.Post(fmt.Sprintf("/repos/%s/docs", baseNamespace), body, resp); err != nil {
			return err
		}
	}
	b.Id = resp.Data.Id
	//b.Slug = resp.Data.Slug
	raw, _ := json.Marshal(resp.Data)
	jsonRaw := json.RawMessage(raw)
	b.Raw = &jsonRaw
	now := time.Now()
	b.UpdatedAt = &now
	return nil
}

func (y *Yuque) Post(action string, body []byte, v interface{}) error {
	req, _ := http.NewRequest(http.MethodPost, y.baseUri+action, bytes.NewReader(body))

	return y.handleRequest(req, v)
}

func (y *Yuque) Put(action string, body []byte, v interface{}) error {
	req, _ := http.NewRequest(http.MethodPut, y.baseUri+action, bytes.NewReader(body))

	return y.handleRequest(req, v)
}

func (y *Yuque) Get(action string, v interface{}) error {
	req, _ := http.NewRequest(http.MethodGet, y.baseUri+action, nil)
	return y.handleRequest(req, v)
}

func (y *Yuque) handleRequest(req *http.Request, v interface{}) error {
	req.Header.Add("User-Agent", "PHP后端规范")
	req.Header.Add("X-Auth-Token", token)
	req.Header.Add("Content-Type", "application/json")
	resp, err := y.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if v != nil {
		bys, _ := ioutil.ReadAll(resp.Body)
		if err := json.Unmarshal(bys, v); err != nil {
			return err
		}
	}
	return nil
}

func saveYuQue(y *Yuque) func(info os.FileInfo, dir string, b *Book) {
	return func(info os.FileInfo, dir string, b *Book) {
		filepath := path.Join(dir, info.Name())
		fi, err := os.Open(filepath)
		if err != nil {
			fmt.Printf("Error: %s\n", err)
			return
		}
		defer fi.Close()

		content := bytes.NewBuffer(nil)

		br := bufio.NewReader(fi)
		idx := 0

		title := ""
		for {
			a, _, c := br.ReadLine()
			if idx == 0 {
				title = string(a[:])
				title = strings.ReplaceAll(title, "#", "")
				title = strings.ReplaceAll(title, " ", "")
				title = strings.ReplaceAll(title, "\n", "")
			}
			if c == io.EOF {
				break
			}
			idx += 1
			content.Write(a)
			content.WriteString("\n")
		}
		// 标题
		b.Title = title

		decode := json.NewDecoder(content)
		decode.DisallowUnknownFields()
		body := &Body{
			Title:  title,
			Public: 0,
			Slug:   b.Slug,
			Format: "markdown",
			Body:   content.String(),
		}

		post, err := json.Marshal(body)
		if err != nil {
			log.Printf("json post err %s", err)
			return
		}
		if err := y.Save(post, b); err != nil {
			log.Println(err)
		}
	}
}

type Body struct {
	Title  string `json:"title"`
	Slug   string `json:"slug"`
	Public int8   `json:"public"`
	Format string `json:"format"`
	Body   string `json:"body"`
}

type DocDetailSerializer struct {
	Id               int64           `json:"id"`
	Slug             string          `json:"slug"`
	Title            string          `json:"title"`
	BookId           int64           `json:"book_id"`
	Book             json.RawMessage `json:"book"`
	UserId           int64           `json:"user_id"`
	User             json.RawMessage `json:"user"`
	Format           string          `json:"format"`
	Body             json.RawMessage `json:"body"`
	BodyDraft        json.RawMessage `json:"body_draft"`
	BodyHtml         string          `json:"body_html"`
	BodyLake         string          `json:"body_lake"`
	CreatorId        int64           `json:"creator_id"`
	Public           int8            `json:"public"`
	Status           int8            `json:"status"`
	LikesCount       int64           `json:"likes_count"`
	CommentsCount    int64           `json:"comments_count"`
	ContentUpdatedAt *time.Time      `json:"content_updated_at"`
	CreatedAt        *time.Time      `json:"content_updated_at"`
	DeletedAt        *time.Time      `json:"content_updated_at"`
	UpdatedAt        *time.Time      `json:"content_updated_at"`
}

type Book struct {
	Id        int64            `json:"id"`
	Hash      string           `json:"hash"`
	Title     string           `json:"title"`
	Name      string           `json:"name"`
	Dir       string           `json:"dir"`
	Slug      string           `json:"slug"`
	Path      string           `json:"path"`
	Version   string           `json:"last_version"`
	UpdatedAt *time.Time       `json:"updated_at"`
	Raw       *json.RawMessage `json:"raw"`
}

type Books struct {
	path          string
	books         []Book
	_idxById      map[int64]*Book
	_idxByIdIdx   map[int64]int
	_idxBySlug    map[string]*Book
	_idxBySlugIdx map[string]int
}

func (bs *Books) AppendBook(book Book) *Books {
	bs.books = append(bs.books, book)
	return bs
}

func (bs *Books) Parse(path string) *Books {
	bs.path = path
	if _, e := os.Stat(path); e != nil {
		return bs
	}
	jsonBooks := make([]Book, 0)
	data, e := ioutil.ReadFile(path)
	if e == nil {
		_ = json.Unmarshal(data, &jsonBooks)
		if len(jsonBooks) > 0 {
			bs.books = jsonBooks
			bs.reloadIdx()
		}
	}
	return bs
}

func (bs *Books) Save(name ...string) error {
	fsname := "book.json"
	if bs.path != "" {
		fsname = bs.path
	}
	if len(name) > 0 {
		fsname = name[0]
	}

	if bs, err := json.Marshal(bs.books); err != nil {
		return err
	} else if err = ioutil.WriteFile(fsname, bs, os.ModePerm); err != nil {
		return err
	}
	return nil
}

func (bs *Books) reloadIdx() {
	for i, book := range bs.books {
		if bs._idxById == nil {
			bs._idxById = make(map[int64]*Book)
			bs._idxByIdIdx = make(map[int64]int)
		}

		if bs._idxBySlug == nil {
			bs._idxBySlug = make(map[string]*Book)
			bs._idxBySlugIdx = make(map[string]int)
		}

		bs._idxById[book.Id] = &book
		bs._idxByIdIdx[book.Id] = i

		bs._idxBySlug[book.Slug] = &book
		bs._idxBySlugIdx[book.Slug] = i
	}
}

func (bs *Books) FindById(id int64) (has bool, idx int, book *Book) {
	book, has = bs._idxById[id]
	idx = bs._idxByIdIdx[id]
	return
}

func (bs *Books) FindBySlug(slug string) (has bool, idx int, book *Book) {
	book, has = bs._idxBySlug[slug]
	idx = bs._idxBySlugIdx[slug]
	return
}

func (bs *Books) ReadEach(handle func(info os.FileInfo, dir string, b *Book)) {
	books := make([]Book, 0)
	for _, b := range bs.books {
		info, e := os.Stat(path.Join(b.Dir, b.Name))
		if e != nil {
			continue
		}
		handle(info, b.Dir, &b)
		books = append(books, b)
	}
	bs.books = books
}

func encodeString(str string) string {
	hash := md5.New()
	_, _ = io.WriteString(hash, str)
	return fmt.Sprintf("%x", hash.Sum(nil))
}
