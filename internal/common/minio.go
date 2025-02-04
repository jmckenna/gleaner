package common

import (
	"fmt"
	"log"

	configTypes "github.com/gleanerio/gleaner/internal/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/spf13/viper"
)

// MinioConnection Set up minio and initialize client
func MinioConnection(v1 *viper.Viper) *minio.Client {
	//mcfg := v1.GetStringMapString("minio")
	mSub := v1.Sub("minio")
	mcfg, err := configTypes.ReadMinioConfig(mSub)
	if err != nil {
		panic(fmt.Errorf("error when  file minio key: %v", err))
	}
	endpoint := fmt.Sprintf("%s:%d", mcfg.Address, mcfg.Port)
	accessKeyID := mcfg.Accesskey
	secretAccessKey := mcfg.Secretkey
	useSSL := mcfg.Ssl

	minioClient, err := minio.New(endpoint,
		&minio.Options{Creds: credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
			Secure: useSSL})
	// minioClient.SetCustomTransport(&http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}})
	if err != nil {
		log.Fatalln(err)
	}
	return minioClient
}
