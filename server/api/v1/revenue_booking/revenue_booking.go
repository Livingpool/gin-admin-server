package revenue_booking

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	api2captcha "github.com/2captcha/2captcha-go"
	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/flipped-aurora/gin-vue-admin/server/model/common/response"
	"github.com/flipped-aurora/gin-vue-admin/server/model/revenue_booking"
	"github.com/gin-gonic/gin"
)

type RevenueBookingApi struct{}

// 台灣區人氣目的地列表（for booking.com主頁面爬蟲）
// >= 1000的要跑兩次
var TaiwanCityList = [][]string{
	{"台北地區", "%3Btop_destinations%3D5240"}, // 164
	{"新北市", "%3Btop_destinations%3D5231"},  // 531
	{"桃園市", "%3Btop_destinations%3D5245"},  // 218
	{"台中地區", "%3Btop_destinations%3D5201"}, // 662
	{"台南地區", "%3Btop_destinations%3D5525"}, // 477
	{"高雄地區", "%3Btop_destinations%3D5200"}, // 244
	{"基隆地區", "%3Btop_destinations%3D5244"}, // 44
	{"新竹縣", "%3Btop_destinations%3D6667"},  // 81
	{"嘉義縣", "%3Btop_destinations%3D5204"},  // 1007
	{"嘉義縣", "%3Btop_destinations%3D5204"},
	{"新竹市", "%3Btop_destinations%3D5231"}, // 532
	{"苗栗縣", "%3Btop_destinations%3D5238"}, // 1063
	{"苗栗縣", "%3Btop_destinations%3D5238"},
	{"彰化縣", "%3Btop_destinations%3D5191"}, // 182
	{"南投縣", "%3Btop_destinations%3D5233"}, // 1005
	{"南投縣", "%3Btop_destinations%3D5233"},
	{"雲林縣", "%3Btop_destinations%3D5237"},  // 833
	{"屏東縣", "%3Btop_destinations%3D1316"},  // 100
	{"宜蘭縣", "%3Btop_destinations%3D1315"},  // 83
	{"花蓮縣", "%3Btop_destinations%3D13633"}, // 592
	{"台東縣", "%3Btop_destinations%3D1312"},  // 131
	{"澎湖縣", "%3Btop_destinations%3D5239"},  // 47
	{"連江縣", "%3Btop_destinations%3D5242"},  // 150
	{"恆春鎮", "%3Btop_destinations%3D5209"},  // 11
	// "金門縣", // 258
}

var counter int = 0

// @Author: 劉庭瑋
// @Function: parseBookingHtml
// @Description: [utility function] 解析booking.com的html，並回傳結果
// @Param: doc *goquery.Document, resultData *[]revenue_booking.ParseHtmlAndSetHotelsInfoToDB
// @Return: []revenue_booking.ParseHtmlAndSetHotelsInfoToDB, error
func parseBookingHtml(doc *goquery.Document, city string) ([]revenue_booking.ParseHtmlAndSetHotelsInfoToDB, error) {

	var err error
	var resultData []revenue_booking.ParseHtmlAndSetHotelsInfoToDB

	// the property cards are in divs with data-testid="property-card"
	doc.Find("div[data-testid='property-card']").Each(func(i int, card *goquery.Selection) {

		// declare a new data variable for each goroutine
		var data revenue_booking.ParseHtmlAndSetHotelsInfoToDB

		data.City = city
		data.Platform = "Booking"

		// extract hotel name
		title := card.Find("div[data-testid='title']").Text()
		data.Name = title

		// extract location
		location := card.Find("span[data-testid='address']").Text()
		data.Location = location

		// extract href
		href, exists := card.Find("a").Attr("href")
		if exists {
			data.Url = href
		} else {
			fmt.Println("Url not found for hotel: ", title)
		}

		// [NESTED SCRAPER] send GET request to hotel details page & extract address, certificate
		var resultHtml2 string
		if err := revenueBookingApiService.DoRequestAndGetResponseWithRetry("GET", href, http.NoBody, "", &resultHtml2); err != nil {
			fmt.Println("DoRequestAndGetResponse failed!")
			return
		}

		doc2, err := goquery.NewDocumentFromReader(strings.NewReader(resultHtml2))
		if err != nil {
			return
		}

		// extract address & add it to location
		address := doc2.Find("span.hp_address_subtitle").Text()
		fmt.Printf("%d. %s ::: %s ::: %s\n", counter, title, data.Location, address)
		data.Location = data.Location + " " + address

		counter++

		// extract license number
		licenseNumber := ""
		doc2.Find("div.page-section.js-k2-hp--block.k2-hp--fine_print p").Each(func(i int, s *goquery.Selection) {
			pText := s.Text()
			if strings.Contains(pText, "執照號碼") {
				index := strings.Index(pText, "\n")
				if index != -1 {
					// If a newline character is found, get the substring from "執照號碼" to the newline character.
					licenseNumber = pText[:index]
				} else {
					// If no newline character is found, get the substring from "執照號碼" to the end of the string.
					index2 := strings.Index(pText, "執照號碼")
					licenseNumber = pText[index2:]
				}
			}
		})

		data.LicenseNumber = licenseNumber
		resultData = append(resultData, data)

		// sleep for 1 second to avoid getting blocked
		time.Sleep(1 * time.Second)
	})

	if err != nil {
		return nil, err
	}

	return resultData, nil
}

// @Author: 劉庭瑋
// @Function: ScrapeBookingMainPageAndSetHotelsInfoToDB
// @Description: Scrape booking.com 主頁面台灣區所有旅館資料（除了飯店）
// @Param: None
// @Reference1: [爬蟲Python範例] https://scrapfly.io/blog/how-to-scrape-bookingcom/
// @Reference2: [爬蟲Golang範例] https://proxiesapi.com/articles/scraping-booking-com-property-listings-in-go-in-2023
// @Note: 台灣地區旅館數量太多，搜尋結果不會全部顯示於網頁(Paging上限為1000筆左右)，目前實作為縮小範圍在各縣市搜尋。
func (r *RevenueBookingApi) ScrapeBookingMainPageAndSetHotelsInfoToDB(c *gin.Context) {

	// set query parameters
	const baseUrl string = "https://www.booking.com/searchresults.zh-tw.html"
	params := url.Values{}
	params.Set("ss", "Taiwan")
	params.Set("dest_type", "country")
	params.Set("dest_id", "206")
	params.Set("lang", "zh-tw")
	params.Set("src", "searchresults")
	params.Set("group_adults", "1")
	params.Set("no_rooms", "1")
	params.Set("group_children", "0")
	// 去除飯店
	nflt := "nflt=" + "ht_id%3D228%3Bht_id%3D208%3Bht_id%3D231%3Bht_id%3D220%3Bht_id%3D221%3Bht_id%3D223%3Bht_id%3D212%3Bht_id%3D201%3Bht_id%3D213%3Bht_id%3D203%3Bht_id%3D222%3Bht_id%3D205%3Bht_id%3D216%3Bht_id%3D206%3Bht_id%3D210%3Bht_id%3D225%3Bht_id%3D214%3Bht_id%3D224%3Bht_id%3D226%3Bht_id%3D235"

	resultUrl := baseUrl + "?" + params.Encode() + "&" + nflt
	fmt.Println(resultUrl)

	// set headers & send GET request to booking.com to get # of hotels in Taiwan
	var resultHtml string
	if err := revenueBookingApiService.DoRequestAndGetResponse("GET", resultUrl, http.NoBody, "", &resultHtml); err != nil {
		fmt.Println("DoRequestAndGetResponse failed!")
		response.ResponseFail(http.StatusBadRequest, err.Error(), c)
		return
	}

	// parse the HTML response using goquery
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resultHtml))
	if err != nil {
		response.ResponseFail(http.StatusInternalServerError, err.Error(), c)
		return
	}

	// save the HTML response for debugging purposes
	html, _ := doc.Html()
	err = os.WriteFile("output.html", []byte(html), 0644)
	if err != nil {
		fmt.Printf("Error writing file: %s\n", err)
		response.ResponseFail(http.StatusInternalServerError, err.Error(), c)
		return
	}

	// extract number of total results (regex involved)
	totalResultsText := doc.Find("div.bcbf33c5c3 div.efdb2b543b h1").Text()
	re := regexp.MustCompile(`[\d,]+`)
	totalResultsString := re.FindString(totalResultsText)
	totalResultsString = strings.ReplaceAll(totalResultsString, ",", "")
	totalResults, err := strconv.Atoi(totalResultsString)
	if err != nil {
		response.ResponseFail(http.StatusInternalServerError, err.Error(), c)
		return
	}

	totalPages := totalResults/25 + 1
	fmt.Println("=== Total # of hotels in Taiwan: ", totalResults, " ===")

	// set up concurrency
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make(chan []revenue_booking.ParseHtmlAndSetHotelsInfoToDB, totalPages)

	// create a semaphore with a capacity equal to the maximum number of concurrent calls
	maxConcurrentCalls := 6
	sem := make(chan struct{}, maxConcurrentCalls)

	// collect the results from the channel in a separate goroutine
	totalResultData := make([]revenue_booking.ParseHtmlAndSetHotelsInfoToDB, 0, totalResults) // preallocate resultData with enough capacity
	var resultsWg sync.WaitGroup
	resultsWg.Add(1)
	go func() {
		defer resultsWg.Done()
		for pageResultData := range results {
			fmt.Println("=== Collecting results ===")
			mu.Lock() // lock the mutex before appending to totalResultData
			totalResultData = append(totalResultData, pageResultData...)
			mu.Unlock() // unlock the mutex after appending to totalResultData
		}
	}()

	// iterates over all cities in TaiwanCityList
	tracker := true
	for _, city := range TaiwanCityList {

		// if totalResults >= 1000, 將民宿分開查詢
		if city[0] == "嘉義縣" || city[0] == "苗栗縣" || city[0] == "南投縣" {
			if !tracker {
				nflt = "nflt=" + "ht_id%3D228%3Bht_id%3D208%3Bht_id%3D231%3Bht_id%3D220%3Bht_id%3D221%3Bht_id%3D223%3Bht_id%3D212%3Bht_id%3D201%3Bht_id%3D213%3Bht_id%3D203%3Bht_id%3D205%3Bht_id%3D216%3Bht_id%3D206%3Bht_id%3D210%3Bht_id%3D225%3Bht_id%3D214%3Bht_id%3D224%3Bht_id%3D226%3Bht_id%3D235" + city[1]
				tracker = false
			} else {
				nflt = "nflt=" + "ht_id%3D222" + city[1]
				tracker = true
			}
		} else {
			nflt = "nflt=" + "ht_id%3D228%3Bht_id%3D208%3Bht_id%3D231%3Bht_id%3D220%3Bht_id%3D221%3Bht_id%3D223%3Bht_id%3D212%3Bht_id%3D201%3Bht_id%3D213%3Bht_id%3D203%3Bht_id%3D222%3Bht_id%3D205%3Bht_id%3D216%3Bht_id%3D206%3Bht_id%3D210%3Bht_id%3D225%3Bht_id%3D214%3Bht_id%3D224%3Bht_id%3D226%3Bht_id%3D235" + city[1]
		}
		params.Set("offset", "0")
		resultUrl := baseUrl + "?" + params.Encode() + "&" + nflt

		fmt.Println(resultUrl)
		fmt.Println("=== current city: ", city[0], " ===")

		// set headers & send GET request to booking.com
		if err := revenueBookingApiService.DoRequestAndGetResponse("GET", resultUrl, http.NoBody, "", &resultHtml); err != nil {
			fmt.Println("DoRequestAndGetResponse failed!")
			response.ResponseFail(http.StatusBadRequest, err.Error(), c)
			return
		}

		// parse the HTML response using goquery
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(resultHtml))
		if err != nil {
			response.ResponseFail(http.StatusInternalServerError, err.Error(), c)
			return
		}

		// save the HTML response for debugging purposes
		html, _ := doc.Html()
		err = os.WriteFile("output.html", []byte(html), 0644)
		if err != nil {
			fmt.Printf("Error writing file: %s\n", err)
			response.ResponseFail(http.StatusInternalServerError, err.Error(), c)
			return
		}

		// extract number of total results (regex involved)
		totalResultsText := doc.Find("div.bcbf33c5c3 div.efdb2b543b h1").Text()
		re := regexp.MustCompile(`[\d,]+`)
		totalResultsString := re.FindString(totalResultsText)
		totalResultsString = strings.ReplaceAll(totalResultsString, ",", "")
		totalResults, err := strconv.Atoi(totalResultsString)
		if err != nil {
			response.ResponseFail(http.StatusInternalServerError, err.Error(), c)
			return
		}

		fmt.Println("=== Total results of city ", city[0], " : ", totalResults, " ===")

		// extract data from the first page
		var resultData []revenue_booking.ParseHtmlAndSetHotelsInfoToDB
		resultData, err = parseBookingHtml(doc, city[0])
		if err != nil {
			response.ResponseFail(http.StatusInternalServerError, err.Error(), c)
			return
		}
		totalResultData = append(totalResultData, resultData...)

		currentPage := 1
		totalPages := totalResults/25 + 1

		// implement full paging
		for currentPage < totalPages {

			sem <- struct{}{} // acquire a token
			wg.Add(1)         // increment the WaitGroup counter

			go func(currentPage int) {

				// decrement the WaitGroup counter when the goroutine completes
				defer wg.Done()

				// release the token when the goroutine completes
				defer func() { <-sem }()

				// set offset & send GET request to booking.com
				params.Set("offset", strconv.Itoa(currentPage*25))
				resultUrl2 := baseUrl + "?" + params.Encode() + "&" + nflt
				var resultHtml2 string
				if err := revenueBookingApiService.DoRequestAndGetResponse("GET", resultUrl2, http.NoBody, "", &resultHtml2); err != nil {
					fmt.Println("DoRequestAndGetResponse failed!")
					response.ResponseFail(http.StatusBadRequest, err.Error(), c)
					return
				}

				fmt.Println(resultUrl2)
				fmt.Println("=== current page: ", currentPage, " === ", "offset: ", currentPage*25, " ===")

				// parse the HTML response using goquery
				doc2, err := goquery.NewDocumentFromReader(strings.NewReader(resultHtml2))
				if err != nil {
					response.ResponseFail(http.StatusInternalServerError, err.Error(), c)
					return
				}

				// extract data from the current page
				pageResultData, err := parseBookingHtml(doc2, city[0])
				if err != nil {
					response.ResponseFail(http.StatusInternalServerError, err.Error(), c)
					return
				}

				// send the results to the channel
				results <- pageResultData

			}(currentPage)

			currentPage++

			// sleep for 1 second to avoid getting blocked
			time.Sleep(1 * time.Second)
		}
	}

	fmt.Println("=== Waiting for goroutines to complete ===")

	counter = 0

	// wait for all goroutines to complete
	wg.Wait()

	// close the channel when all goroutines are completed
	close(results)

	fmt.Println("=== All goroutines completed ===")

	// Wait for the results collecting goroutine to finish before saving the data in the database
	resultsWg.Wait()

	fmt.Println("=== Saving data in the database ===")

	fmt.Printf("=== Collected %d entries ===\n", len(totalResultData))

	// save data in DB.
	if err := revenueBookingApiService.SaveHotelsRecord(&totalResultData); err != nil {
		fmt.Println("SaveHotelsInfoRecord failed!")
		response.ResponseFail(http.StatusForbidden, err.Error(), c)
		return
	}

	response.ResponseSuccess(http.StatusOK, "SaveHotelsInfoRecord completed!", c)
}

// @Author: 劉庭瑋
// @Function: RailBikeScraper
// @Description: [未完成]爬取舊山路鐵道自行車網站的資料，並解析驗證碼，若有空位則發送email通知
// @Param: None
func RailBikeScraper() {

	// create context
	ctx, cancel := chromedp.NewContext(
		context.Background(),
		chromedp.WithLogf(log.Printf),
	)
	defer cancel()

	// create a timeout as a safety net to prevent any infinite wait loops
	ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// enable network domain
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		log.Fatal("network enable(): ", err)
	}

	// this will be used to capture the request id for matching network events
	var requestID network.RequestID

	// set up a channel, so we can block later while we monitor the download progress
	done := make(chan bool)

	// set up a listener to watch the network events and close the channel when
	// complete the request id matching is important both to filter out
	// unwanted network events and to reference the downloaded file later
	chromedp.ListenTarget(ctx, func(event interface{}) {
		switch ev := event.(type) {
		case *network.EventRequestWillBeSent:
			log.Printf("EventRequestWillBeSent: %v: %v", ev.RequestID, ev.Request.URL)
			// check if this is the request we're interested in
			if ev.Request.URL == "https://www.oml-railbike.com/formcode/formcode.php" {
				requestID = ev.RequestID
			}
		case *network.EventLoadingFinished:
			log.Printf("EventLoadingFinished: %v", ev.RequestID)
			if ev.RequestID == requestID {
				close(done)
			}
		}
	})

	// all we need to do here is navigate to the download url
	err := chromedp.Run(ctx, chromedp.Tasks{
		chromedp.Navigate(`https://www.oml-railbike.com/_pages/order/step2.php?u=common`),
	})
	if err != nil {
		log.Fatal("navigate(): ", err)
	}

	// This will block until the chromedp listener closes the channel
	<-done

	// get the downloaded bytes for the request id
	var buf []byte
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		buf, err = network.GetResponseBody(requestID).Do(ctx)
		return err
	})); err != nil {
		log.Fatal(err)
	}

	// write the file to disk - since we hold the bytes we dictate the name and
	// location
	if err := os.WriteFile("download.png", buf, 0644); err != nil {
		log.Fatal(err)
	}
	log.Print("wrote download.png")

	// solve captcha
	client := api2captcha.NewClient("b8f59b3a627be8eced7892f3795e1da0")

	balance, err := client.GetBalance()
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Balance:", balance)

	cap := api2captcha.Normal{
		File: "download.png",
	}

	code, err := client.Solve(cap.ToRequest())
	if err != nil {
		if err == api2captcha.ErrTimeout {
			log.Fatal("Timeout")
		} else if err == api2captcha.ErrApi {
			log.Fatal("API error")
		} else if err == api2captcha.ErrNetwork {
			log.Fatal("Network error")
		} else {
			log.Fatal(err)
		}
	}

	log.Println("Captcha code:", code)

	err = chromedp.Run(ctx,
		chromedp.SendKeys(`#chknum`, code),
		chromedp.Click(`button[onclick="stepcode()"]`),
		chromedp.WaitReady(`#calendar`),
		chromedp.Click(`input[type="radio"][value="BTOC"]`, chromedp.NodeVisible),
		chromedp.Click(`#calendar button[aria-label="next"]`, chromedp.NodeVisible),
		chromedp.WaitReady(`table .fc-event-container`),
		chromedp.Click(`#calendar td[data-date="2024-02-04"]`, chromedp.NodeVisible),
	)
	if err != nil {
		log.Fatal(err)
	}
}
