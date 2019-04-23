package zulip

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
)

type EventListener interface {
	HandleEvent(*EventResponse) bool
}

type Zulip struct {
	authLogin, authPass string
	baseUrl             string
	queueID             string
	Debug               bool
}

func NewZulipApi(baseUrl string) *Zulip {
	return &Zulip{baseUrl: baseUrl}
}

func (z *Zulip) SetBasicAuth(login, pass string) {
	z.authLogin = login
	z.authPass = pass
}

func (z *Zulip) tryToCallApi(url, method string, params url.Values) []byte {
	client := &http.Client{}

	url = fmt.Sprintf("%s/%s?%s", z.baseUrl, url, params.Encode())
	if z.Debug {
		fmt.Println(url)
	}

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return []byte{}
	}
	req.SetBasicAuth(z.authLogin, z.authPass)

	resp, err := client.Do(req)
	if err != nil {
		return []byte{}
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return []byte{}
	}

	return body
}

func (z *Zulip) api(url, method string, params url.Values) (bytes []byte, err error) {
	for i := 0; i <= 5; i++ {
		bytes = z.tryToCallApi(url, method, params)

		var res BaseResponse
		err = json.Unmarshal(bytes, &res)
		if err != nil {
			if z.Debug {
				fmt.Println("Failed to parse response")
			}
			time.Sleep(time.Second)
			continue
		}
		if z.Debug {
			spew.Dump(res)
		}

		if res.Result == "error" {
			if strings.HasPrefix(res.Msg, "API usage exceeded rate limit") {
				if z.Debug {
					fmt.Println("Exceeded API rate limit, sleeping for 1 second")
				}
				time.Sleep(time.Second)
				continue
			}
		}
		return
	}
	return
}

func (z *Zulip) Register(event_types []string) string {
	v := url.Values{}
	json_types, _ := json.Marshal(event_types)
	v.Set("event_types", string(json_types))

	bytes, err := z.api("api/v1/register", "POST", v)
	if err != nil {
		panic(err)
	}

	var res RegisterResponse
	err = json.Unmarshal(bytes, &res)
	if err != nil {
		panic(err)
	}

	z.queueID = res.QueueID
	return res.QueueID
}

func (z *Zulip) tryToGetEvents(last_event_id string) []byte {
	v := url.Values{}
	v.Set("queue_id", z.queueID)
	v.Set("last_event_id", last_event_id)

	res, err := z.api("api/v1/events", "GET", v)
	if err != nil {
		panic(err)
	}
	if z.Debug {
		fmt.Printf("Event: %s", res)
	}
	return res
}

func (z *Zulip) GetEvents(handler EventListener) {
	var last_event_id int64 = -1
	for {
		bytes := z.tryToGetEvents(strconv.FormatInt(last_event_id, 10))
		var res EventsResponse
		err := json.Unmarshal(bytes, &res)
		if err != nil {
			panic(err)
		}

		if z.Debug {
			log.Printf("Event with ID: %s\n", res.QueueID)
		}
		if res.Result != "success" {
			if z.Debug {
				log.Printf("Result != success: %s\n", res.Result)
			}
			panic(res.Msg)
		}
		events := res.Events
		for _, event := range events {
			if event.ID > last_event_id {
				last_event_id = event.ID
			}
			if event.Type == "heartbeat" {
				if z.Debug {
					log.Println("Heartbeat received")
				}
				continue
			}
			result := handler.HandleEvent(event)
			if !result {
				return
			}
		}
	}
}
