package acquire

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"earthcube.org/Project418/gleaner/internal/common"
	"earthcube.org/Project418/gleaner/pkg/summoner/sitemaps"
	"earthcube.org/Project418/gleaner/pkg/utils"
	"github.com/PuerkitoBio/goquery"
	"github.com/gosuri/uiprogress"
	"github.com/kazarena/json-gold/ld"
	"github.com/minio/minio-go"
)

// ResRetrieve is a function to pull down the data graphs at resources
func ResRetrieve(mc *minio.Client, m map[string]sitemaps.URLSet, cs utils.Config) {
	uiprogress.Start()
	wg := sync.WaitGroup{}

	for k := range m {
		log.Printf("Queuing URLs for %s \n", k)
		go getDomain(mc, m, k, &wg)
	}

	time.Sleep(2 * time.Second)
	wg.Wait()
	uiprogress.Stop()
}

func getDomain(mc *minio.Client, m map[string]sitemaps.URLSet, k string, wg *sync.WaitGroup) {
	semaphoreChan := make(chan struct{}, 10) // a blocking channel to keep concurrency under control
	defer close(semaphoreChan)
	lwg := sync.WaitGroup{}

	wg.Add(1)       // wg from the calling function
	defer wg.Done() // tell the wait group that we be done

	count := len(m[k].URL)
	bar := uiprogress.AddBar(count).PrependElapsed().AppendCompleted()
	bar.PrependFunc(func(b *uiprogress.Bar) string {
		return rightPad2Len(k, " ", 25)
	})
	bar.Fill = '-'
	bar.Head = '>'
	bar.Empty = ' '

	// if count < 1 {
	// 	log.Printf("No resources found for %s \n", k)
	// 	return // should maked this return an error
	// }

	var (
		buf    bytes.Buffer
		logger = log.New(&buf, "logger: ", log.Lshortfile)
	)

	for i := range m[k].URL {
		lwg.Add(1)
		urlloc := m[k].URL[i].Loc

		go func(i int, k string) {

			// logger approach to buffer in core lib (use for sending logs to s3 in web ui)

			semaphoreChan <- struct{}{}

			var client http.Client
			req, err := http.NewRequest("GET", urlloc, nil)
			if err != nil {
				logger.Printf("#%d error on %s : %s  ", i, urlloc, err) // print an message containing the index (won't keep order)
			}

			req.Header.Set("User-Agent", "EarthCube_DataBot/1.0")

			resp, err := client.Do(req)
			if err != nil {
				logger.Printf("#%d error on %s : %s  ", i, urlloc, err) // print an message containing the index (won't keep order)
				lwg.Done()                                              // tell the wait group that we be done
				<-semaphoreChan
				return
			}
			defer resp.Body.Close()

			doc, err := goquery.NewDocumentFromResponse(resp)
			if err != nil {
				logger.Printf("#%d error on %s : %s  ", i, urlloc, err) // print an message containing the index (won't keep order)
				lwg.Done()                                              // tell the wait group that we be done
				<-semaphoreChan
				return
			}

			var jsonld string
			if err == nil {
				doc.Find("script").Each(func(i int, s *goquery.Selection) {
					val, _ := s.Attr("type")
					if val == "application/ld+json" {
						action, err := isValid(s.Text())
						if err != nil {
							logger.Printf("ERROR: URL: %s Action: %s  Error: %s", urlloc, action, err)
						}
						jsonld = s.Text()
					}
				})
			}

			if jsonld != "" { // traps out the root domain...   should do this different
				sha, err := common.GetNormSHA(jsonld) // Moved to the normalized sha value
				if err != nil {
					logger.Printf("ERROR: URL: %s Action: Getting normalized sha  Error: %s\n", urlloc, err)
				}
				objectName := fmt.Sprintf("%s/%s.jsonld", k, sha)
				contentType := "application/ld+json"
				b := bytes.NewBufferString(jsonld)

				usermeta := make(map[string]string) // what do I want to know?
				usermeta["url"] = urlloc
				usermeta["sha1"] = sha
				bucketName := "gleaner-summoned" //   fmt.Sprintf("gleaner-summoned/%s", k) // old was just k

				// Upload the file with FPutObject
				_, err = mc.PutObject(bucketName, objectName, b, int64(b.Len()), minio.PutObjectOptions{ContentType: contentType, UserMetadata: usermeta})
				if err != nil {
					logger.Printf("%s", objectName)
					logger.Fatalln(err) // Fatal?   seriously?    I guess this is the object write, so the run is likely a bust at this point, but this seems a bit much still.
				}
				// logger.Printf("#%d Uploaded Bucket:%s File:%s Size %d\n", i, bucketName, objectName, n)
			}

			bar.Incr()

			// logger.Printf("#%d thread for %s ", i, urlloc) // print an message containing the index (won't keep order)
			lwg.Done() // tell the wait group that we be done

			<-semaphoreChan // clear a spot in the semaphore channel
		}(i, k)

	}

	// return the logger buffer or write to a mutex locked bytes buffer
	f, err := os.Create(fmt.Sprintf("./%s.log", k))
	if err != nil {
		log.Println("Error writing a file")
	}

	w := bufio.NewWriter(f)
	_, err = w.WriteString(buf.String())
	if err != nil {
		log.Println("Error writing a file")
	}
	w.Flush()

	lwg.Wait()

}

func rightPad2Len(s string, padStr string, overallLen int) string {
	var padCountInt int
	padCountInt = 1 + ((overallLen - len(padStr)) / len(padStr))
	var retStr = s + strings.Repeat(padStr, padCountInt)
	return retStr[:overallLen]
}

// ResRetrieveOLD is a function to pull down the data graphs at resources
func ResRetrieveOLD(mc *minio.Client, m map[string]sitemaps.URLSet, cs utils.Config) {
	// err := buildBuckets(mc, m) // TODO needs error obviously
	// if err != nil {
	// 	log.Printf("Gleaner bucket report:  %s", err)
	// }

	// TODO  add in a bytes buffer to store logging information

	// set up some concurrency support
	semaphoreChan := make(chan struct{}, 20) // a blocking channel to keep concurrency under control
	defer close(semaphoreChan)
	wg := sync.WaitGroup{} // a wait group enables the main process a wait for goroutines to finish

	for k := range m {
		log.Printf("Queuing URLs for %s \n", k)
		count := len(m[k].URL)

		if count < 1 {
			log.Printf("No resources found for %s \n", k)
			break
		}

		// TODO review why this isn't in the go func...
		// https://github.com/gosuri/uiprogress/blob/master/example/incr/incr.go
		// bar := uiprogress.AddBar(count).AppendCompleted().PrependElapsed()
		// bar.PrependFunc(func(b *uiprogress.Bar) string {
		// 	return fmt.Sprintf("Task (%d/%d)", b.Current(), count)
		// })
		// uiprogress.Start() // start rendering

		for i := range m[k].URL {
			wg.Add(1)
			urlloc := m[k].URL[i].Loc

			go func(i int, k string) {
				semaphoreChan <- struct{}{}

				var client http.Client
				req, err := http.NewRequest("GET", urlloc, nil)
				if err != nil {
					log.Printf("#%d error on %s : %s  ", i, urlloc, err) // print an message containing the index (won't keep order)
				}

				req.Header.Set("User-Agent", "EarthCube_DataBot/1.0")

				resp, err := client.Do(req)
				if err != nil {
					log.Printf("#%d error on %s : %s  ", i, urlloc, err) // print an message containing the index (won't keep order)
					wg.Done()                                            // tell the wait group that we be done
					<-semaphoreChan
					return
				}
				defer resp.Body.Close()

				doc, err := goquery.NewDocumentFromResponse(resp)
				if err != nil {
					log.Printf("#%d error on %s : %s  ", i, urlloc, err) // print an message containing the index (won't keep order)
					wg.Done()                                            // tell the wait group that we be done
					<-semaphoreChan
					return
				}

				// TODO Version that just looks for script type application/ld+json
				// this will look for ALL nodes in the doc that match, there may be more than one
				var jsonld string
				if err == nil {
					doc.Find("script").Each(func(i int, s *goquery.Selection) {
						val, _ := s.Attr("type")
						if val == "application/ld+json" {
							action, err := isValid(s.Text())
							if err != nil {
								log.Printf("ERROR: URL: %s Action: %s  Error: %s", urlloc, action, err)
							}
							jsonld = s.Text()
						}
					})
				}

				if jsonld != "" { // traps out the root domain...   should do this different
					sha, err := common.GetNormSHA(jsonld) // Moved to the normalized sha value
					if err != nil {
						log.Printf("ERROR: URL: %s Action: Getting normalized sha  Error: %s\n", urlloc, err)
					}
					objectName := fmt.Sprintf("%s/%s.jsonld", k, sha)
					contentType := "application/ld+json"
					b := bytes.NewBufferString(jsonld)

					usermeta := make(map[string]string) // what do I want to know?
					usermeta["url"] = urlloc
					usermeta["sha1"] = sha
					bucketName := "gleaner-summoned" //   fmt.Sprintf("gleaner-summoned/%s", k) // old was just k

					// Upload the file with FPutObject
					_, err = mc.PutObject(bucketName, objectName, b, int64(b.Len()), minio.PutObjectOptions{ContentType: contentType, UserMetadata: usermeta})
					if err != nil {
						log.Printf("%s", objectName)
						log.Fatalln(err) // Fatal?   seriously?    I guess this is the object write, so the run is likely a bust at this point, but this seems a bit much still.
					}
					// log.Printf("#%d Uploaded Bucket:%s File:%s Size %d\n", i, bucketName, objectName, n)
				}

				// log.Printf("#%d thread for %s ", i, urlloc) // print an message containing the index (won't keep order)
				wg.Done() // tell the wait group that we be done
				// bar.Incr()

				<-semaphoreChan // clear a spot in the semaphore channel
			}(i, k)
		}
	}

	wg.Wait() // wait for all the goroutines to be done
	// uiprogress.Stop() // how should I be doing this?

}

func isValid(jsonld string) (string, error) {
	proc := ld.NewJsonLdProcessor()
	options := ld.NewJsonLdOptions("")
	options.Format = "application/nquads"

	var myInterface interface{}
	action := ""

	err := json.Unmarshal([]byte(jsonld), &myInterface)
	if err != nil {
		action = "json.Unmarshal call"
		return action, err
	}

	_, err = proc.ToRDF(myInterface, options) // returns triples but toss them, just validating
	if err != nil {
		action = "JSON-LD to RDF call"
		return action, err
	}

	return action, err
}
