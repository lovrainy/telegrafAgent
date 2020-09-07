package api

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
)

func fileList(dirname string) (*map[string]int64, error) {
	files := make(map[string]int64)
	err := filepath.Walk(dirname,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			files[path] = info.Size()
			return nil
		})
	if err != nil {
		return &files, nil
	}
	return &files, nil
}

type FileInfo struct {
	File string `json:"file"`
	Info *[]map[string]string
}

func FileListHandler(w http.ResponseWriter, r *http.Request) {
	//fmt.Println(r.URL.Query())
	dirs := r.URL.Query().Get("data")
	files, err := fileList(dirs)
	if err != nil {
		w.WriteHeader(401)
		w.Write([]byte(fmt.Sprintf("%s", err)))
	}
	f, err := json.Marshal(files)
	w.Write([]byte(fmt.Sprintf("%s", f)))
}

func FileDeleteHandler(w http.ResponseWriter, r *http.Request) {
	var data map[string]string
	a1, _ := ioutil.ReadAll(r.Body)
	json.Unmarshal(a1, &data)
	err := os.Remove(data["data"])
	if err != nil {
		//删除失败
		w.WriteHeader(400)
		w.Write([]byte(fmt.Sprintf("%s", err)))
	}else{
		//删除成功
		w.WriteHeader(200)
		w.Write([]byte(fmt.Sprintf("success")))
	}
}
