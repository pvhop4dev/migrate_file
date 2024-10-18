package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"os"
	"path/filepath"

	"github.com/hirochachacha/go-smb2"
	"github.com/spf13/viper"
)

func init() {
	logFile, err := os.OpenFile("app.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Panic(fmt.Errorf("lỗi khi mở tệp log %s", err))
	}
	multiWriter := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(multiWriter)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)
	log.Printf("-------------------------------START-----------------------------------")
}

const up = "PUT_FILE"
const down = "GET_FILE"

func main() {
	// viper.SetConfigName("config")
	// viper.SetConfigType("yaml")
	// viper.AddConfigPath(".")
	configFile := flag.String("config", "config.yml", "Đường dẫn đến file cấu hình")
	log.Println("Đang đọc file cấu hình: ", *configFile)
	flag.Parse()
	viper.SetConfigFile(*configFile)

	if err := viper.ReadInConfig(); err != nil {
		log.Panic(fmt.Errorf("lỗi khi đọc file cấu hình %s", err))
	}

	host := viper.GetString("samba.host")
	port := viper.GetInt("samba.port")
	user := viper.GetString("samba.user")
	password := viper.GetString("samba.password")
	shareFolder := viper.GetString("samba.share")
	folders := viper.GetStringSlice("folders")
	direction := viper.GetString("direction")

	share := ConnectSamba(host, port, user, password, shareFolder)
	defer share.Umount()

	// Relocate file
	switch direction {
	case up:
		for _, folder := range folders {
			PutFolder(folder, share)
		}
	case down:
		for _, folder := range folders {
			GetFolder(folder, share)
		}
	default:
		log.Panic("Direction không hợp lệ")
	}
	log.Println("-------------------------------DONE-----------------------------------")
}

func ConnectSamba(host string, port int, user string, password string, shareFolder string) *smb2.Share {
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		log.Panic(fmt.Errorf("lỗi khi kết nối đến server %s:%d err:%s", host, port, err))
	}
	// defer conn.Close()

	d := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     user,
			Password: password,
		},
	}

	s, err := d.Dial(conn)
	if err != nil {
		log.Panic(fmt.Errorf("lỗi khi kết nối đến server %s:%d err:%s", host, port, err))
	}
	// defer s.Logoff()

	share, err := s.Mount(shareFolder)
	if err != nil {
		log.Panic(fmt.Errorf("lỗi khi mount thư mục %s err:%s", shareFolder, err))
	}
	return share
}

func PutFolder(folder string, share *smb2.Share) {
	log.Println("Đang đẩy file trong folder: ", folder)
	files, err := os.ReadDir(folder)
	if err != nil {
		log.Panic(fmt.Errorf("lỗi khi đọc thư mục %s err:%s", folder, err))
	}

	for _, file := range files {
		if !file.IsDir() {
			localFilePath := filepath.Join(folder, file.Name())
			remoteFilePath := filepath.Join(folder, file.Name())
			PutFile(share, file, localFilePath, remoteFilePath)
		}
	}
}

func PutFile(share *smb2.Share, file fs.DirEntry, localFilePath string, remoteFilePath string) {
	// Tạo thư mục nếu không tồn tại
	dirPath := filepath.Dir(remoteFilePath)
	err := share.MkdirAll(dirPath, 0755)
	if err != nil {
		log.Printf("lỗi khi tạo thư mục %s err:%s", dirPath, err)
		return
	}
	// Mở tệp cục bộ
	localFile, err := os.Open(localFilePath)
	if err != nil {
		log.Printf("lỗi khi mở tệp cục bộ %s err:%s", localFilePath, err)
		return
	}
	// defer localFile.Close()

	// Tạo tệp trong thư mục Samba
	remoteFile, err := share.Create(remoteFilePath)
	if err != nil {
		log.Printf("lỗi khi tạo tệp %s err:%s", remoteFilePath, err)
		return // Exit the function if the remote file cannot be created
	}
	defer remoteFile.Close()

	// Sao chép nội dung tệp
	_, err = io.Copy(remoteFile, localFile)
	if err != nil {
		log.Printf("lỗi khi sao chép tệp %s err:%s", remoteFilePath, err)
		return // Exit the function if the file copy fails
	}
	localFile.Close()
	err = os.Remove(localFilePath)
	if err != nil {
		log.Printf("lỗi khi xóa tệp cục bộ %s err:%s", localFilePath, err)
		return
	}

	log.Println("Đã đẩy lên tệp và xóa tệp ở máy nội bộ:", file.Name())
}

func GetFolder(folder string, share *smb2.Share) {
	log.Println("Đang tải về file trong folder: ", folder)
	files, err := share.ReadDir(folder)
	if err != nil {
		log.Panic(fmt.Errorf("lỗi khi đọc thư mục %s err:%s", folder, err))
	}

	for _, file := range files {
		if !file.IsDir() {
			remoteFilePath := filepath.Join(folder, file.Name())
			localFilePath := filepath.Join(folder, file.Name())
			GetFile(share, file, localFilePath, remoteFilePath)
		}
	}
}

func GetFile(share *smb2.Share, file fs.FileInfo, localFilePath string, remoteFilePath string) {
	// Mở tệp từ thư mục Samba
	remoteFile, err := share.Open(remoteFilePath)
	if err != nil {
		log.Panic(fmt.Errorf("lỗi khi mở tệp %s err:%s", remoteFilePath, err))
	}

	dirPath := filepath.Dir(localFilePath)
	err = os.MkdirAll(dirPath, 0755)
	if err != nil {
		log.Printf("lỗi khi tạo thư mục %s err:%s", dirPath, err)
		return
	}
	// Tạo tệp cục bộ
	localFile, err := os.Create(localFilePath)
	if err != nil {
		log.Panic(fmt.Errorf("lỗi khi tạo tệp cục bộ %s err:%s", localFilePath, err))
		remoteFile.Close()
		return
	}

	// Sao chép nội dung tệp
	_, err = io.Copy(localFile, remoteFile)
	if err != nil {
		log.Panic(fmt.Errorf("lỗi khi sao chép tệp %s err:%s", remoteFilePath, err))
	}

	// Đóng tệp
	remoteFile.Close()
	localFile.Close()

	// Xóa tệp trong thư mục Samba
	err = share.Remove(remoteFilePath)
	if err != nil {
		log.Panic(fmt.Errorf("lỗi khi xóa tệp %s err:%s", remoteFilePath, err))
	}

	log.Println("Đã tải về và xóa tệp trên máy từ xa:", file.Name())
}
