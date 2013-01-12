package alicloud

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
)

func (c *Client) CreateFile(dirId int64, file string) (*FileInfo, error) {
	filename := filepath.Base(file)
	extension := filepath.Ext(file)
	tmp, err := os.Stat(file)
	if err != nil {
		panic(err)
	}
	size := tmp.Size()
	modTime := tmp.ModTime().Unix()
	ext := extension
	if len(ext) > 0 {
		ext = ext[1:]
	}
	chunks, md5 := makeChunks(file)
	f := &FileInfo{
		DirId:        dirId,
		FileName:     filename[0 : len(filename)-len(extension)],
		ChangedBy:    61401,
		Extension:    ext,
		FullName:     filename,
		Md5:          md5,
		PlatformInfo: 0,
		Size:         size,
		ModifyTime:   modTime,
		Chunks:       chunks,
	}

	j, _ := json.Marshal(f)

	params := &url.Values{}
	params.Set("file", string(j))
	result, err := c.PostCall("/upload/create", params)
	if err != nil {
		return nil, err
	}
	var fileInfo FileInfo
	json.Unmarshal(result, &fileInfo)
	return &fileInfo, err
}

func makeChunks(file string) ([]*Chunk, string) {
	f, err := os.Open(file)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	r := bufio.NewReader(f)
	buf := make([]byte, DEFAULT_CHUNK_SIZE)

	index := 1
	h := md5.New()
	chunks := make([]*Chunk, 0)
	for {
		n, err := r.Read(buf)
		if err != nil && err != io.EOF {
			panic(err)
		}
		if n == 0 {
			break
		}

		h.Write(buf[0:n])
		pre := index - 1
		if index == 1 {
			pre = -1
		}
		chunk := &Chunk{
			Md5:       md5_bytes(buf, n),
			CheckSum:  checksum_bytes(buf, n),
			Operation: 1,
			Size:      int64(n),
			GenerNext: true,
			GenerPre:  true,
			Pre:       int64(pre),
			Index:     int64(index),
			Next:      int64(index + 1),
		}
		chunks = append(chunks, chunk)
		index = index + 1
	}
	md5_str := hex.EncodeToString(h.Sum(nil))
	chunks[len(chunks)-1].Next = -1
	return chunks, md5_str
}

func (c *Client) UploadChunk(chunkId int64, file string, offset int64, length int64) (bool, error) {
	if length > DEFAULT_CHUNK_SIZE {
		panic(fmt.Sprintf("%s is larger than the default chunk size %s", length/1024/1024, DEFAULT_CHUNK_SIZE/1024/1024))
	}
	f, err := os.Open(file)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	stat, _ := f.Stat()
	if stat.Size() < offset+length {
		panic(fmt.Sprintf("illegal argument offset: %d + size: %d > file length: %d", offset, length, stat.Size()))
	}

	f.Seek(offset, os.SEEK_SET)

	buf := make([]byte, length)
	f.Read(buf)

	params := &url.Values{}
	params.Set("chunkId", fmt.Sprintf("%d", chunkId))
	params.Set("size", fmt.Sprintf("%d", length))

	result, _ := c.UploadCall("/upload/chunk", params, "chunk", filepath.Base(file), bytes.NewReader(buf))
	if bytes.Contains(result, []byte("true")) {
		return true, nil
	}
	return false, nil
}

func (c *Client) CommitUpload(id int64, version int64) (*FileInfo, error) {
	params := &url.Values{}
	params.Set("id", fmt.Sprintf("%d", id))
	params.Set("version", fmt.Sprintf("%d", version))
	result, err := c.PostCall("/upload/commit", params)
	if err != nil {
		return nil, err
	}
	var fileInfo FileInfo
	json.Unmarshal(result, &fileInfo)
	return &fileInfo, err
}

func (c *Client) FileRemove(id int64) (*FileInfo, error) {
	params := &url.Values{}
	params.Set("id", fmt.Sprintf("%d", id))
	result, err := c.PostCall("/file/remove", params)
	if err != nil {
		return nil, err
	}
	var fileInfo FileInfo
	json.Unmarshal(result, &fileInfo)
	return &fileInfo, err
}

func (c *Client) FileInfo(fileId int64, fullName string, operation int) (*FileInfo, error) {
	params := &url.Values{}
	params.Set("fileId", fmt.Sprintf("%d", fileId))
	params.Set("fullName", fullName)
	params.Set("operation", fmt.Sprintf("%d", operation))
	result, err := c.PostCall("/file/info", params)
	if err != nil {
		return nil, err
	}
	var fileInfo FileInfo
	json.Unmarshal(result, &fileInfo)
	return &fileInfo, err
}

func (c *Client) DownloadChunk(chunkId int64) ([]byte, error) {
	params := &url.Values{}
	params.Set("chunkId", fmt.Sprintf("%d", chunkId))
	return c.GetCall("/download/chunk", params)
}

func (c *Client) DownloadFile(fileInfo *FileInfo, dirPath string) {
	f, err1 := os.OpenFile(filepath.Join(c.LocalBaseDir, dirPath, fileInfo.FileName+"."+fileInfo.Extension),
		os.O_RDWR|os.O_CREATE|os.O_TRUNC, os.FileMode(fileInfo.FileAttribute))
	if err1 != nil {
		panic(err1)
	}
	defer f.Close()

	w := bufio.NewWriterSize(f, 1024*8)

	for _, chunk := range fileInfo.Chunks {
		bytes, _ := c.DownloadChunk(chunk.Id)
		s := string(bytes[1 : len(bytes)-1])
		bytes, _ = base64.StdEncoding.DecodeString(s)
		n, err := w.Write(bytes)
		if err != nil || n != len(bytes) {
			panic(err)
		}
		w.Flush()
	}
}

func (c *Client) DownloadFolder(dirId int64, dirPath string) {
	fileList, _ := c.FolderList(dirId)
	for _, file := range fileList.Files {
		fileInfo, _ := c.FileInfo(file.Id, "", 3)
		debug(filepath.Join(dirPath, fileInfo.FileName+"."+fileInfo.Extension))
		c.DownloadFile(fileInfo, dirPath)
	}

	for _, dir := range fileList.Dirs {
		debug(filepath.Join(dirPath, dir.Name))
		c.DownloadFolder(dir.Id, filepath.Join(dirPath, dir.Name))
	}
}
