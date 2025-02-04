package acquire

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/gleanerio/gleaner/internal/common"
	minio "github.com/minio/minio-go/v7"
	"github.com/spf13/viper"
)

/// A utility to keep a list of JSON-LD files that we have found
// in or on a page
func addToJsonListIfValid(v1 *viper.Viper, jsonlds []string, new_json string) ([]string, error) {
	valid, err := isValid(v1, new_json)
	if err != nil {
		return jsonlds, fmt.Errorf("error checking for valid json: %s", err)
	}
	if !valid {
		return jsonlds, fmt.Errorf("invalid json; continuing")
	}
	return append(jsonlds, new_json), nil
}

/// Validate JSON-LD that we get
func isValid(v1 *viper.Viper, jsonld string) (bool, error) {
	proc, options := common.JLDProc(v1)

	var myInterface map[string]interface{}

	err := json.Unmarshal([]byte(jsonld), &myInterface)
	if err != nil {
		return false, fmt.Errorf("Error in unmarshaling json: %s", err)
	}

	_, err = proc.ToRDF(myInterface, options) // returns triples but toss them, just validating
	if err != nil {                           // it's wasted cycles.. but if just doing a summon, needs to be done here
		return false, fmt.Errorf("Error in JSON-LD to RDF call: %s", err)
	}

	return true, nil
}

func Upload(v1 *viper.Viper, mc *minio.Client, logger *log.Logger, bucketName string, site string, urlloc string, jsonld string) (string, error) {
	sha, err := common.GetNormSHA(jsonld, v1) // Moved to the normalized sha value
	if err != nil {
		logger.Printf("ERROR: URL: %s Action: Getting normalized sha  Error: %s\n", urlloc, err)
	}
	objectName := fmt.Sprintf("summoned/%s/%s.jsonld", site, sha)
	contentType := "application/ld+json"
	b := bytes.NewBufferString(jsonld)
	// size := int64(b.Len()) // gets set to 0 after upload for some reason

	usermeta := make(map[string]string) // what do I want to know?
	usermeta["url"] = urlloc
	usermeta["sha1"] = sha

	// write the prov entry for this object
	err = StoreProvNG(v1, mc, site, sha, urlloc, "milled")
	if err != nil {
		logger.Println(err)
	}

	// Upload the file with FPutObject
	_, err = mc.PutObject(context.Background(), bucketName, objectName, b, int64(b.Len()), minio.PutObjectOptions{ContentType: contentType, UserMetadata: usermeta})
	if err != nil {
		logger.Printf("%s", objectName)
		logger.Fatalln(err) // Fatal?   seriously?    I guess this is the object write, so the run is likely a bust at this point, but this seems a bit much still.
	}

	return sha, err

	// logger.Printf("Uploaded Bucket:%s File:%s Size %d\n", bucketName, objectName, size)
}
