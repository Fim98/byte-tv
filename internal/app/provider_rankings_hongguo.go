package app

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var hongguoRankingMetric = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?[万亿]?热度$`)
var hongguoRankingCanonical = regexp.MustCompile(`(?is)<link\b[^>]*>`)
var hongguoRankingPagination = regexp.MustCompile(`(?is)<nav\b[^>]*\baria-label=["']榜单分页["'][^>]*>(.*?)</nav>`)
var hongguoRankingCurrentPage = regexp.MustCompile(`(?is)<[^>]*\baria-current=["']page["'][^>]*>(.*?)</[^>]+>`)
var hongguoRankingAnchor = regexp.MustCompile(`(?is)<a\b[^>]*>(.*?)</a>`)
var hongguoRankingArticle = regexp.MustCompile(`(?is)<article\b[^>]*\baria-labelledby=["']rank-title-[^"']+["'][^>]*>.*?</article>`)
var hongguoRankingHeading = regexp.MustCompile(`(?is)<h[23]\b[^>]*\bid=["']([^"']+)["'][^>]*>(.*?)</h[23]>`)
var hongguoRankingParagraph = regexp.MustCompile(`(?is)<p\b[^>]*>(.*?)</p>`)
var hongguoRankingSpan = regexp.MustCompile(`(?is)<span\b[^>]*>(.*?)</span>`)
var hongguoRankingTags = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>|<style\b[^>]*>.*?</style>|<[^>]+>`)

func (d *Downloader) fetchHongguoRankingPage(ctx context.Context, board rankingBoard, page int) (rankingPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(attempt*attempt) * 300 * time.Millisecond)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return rankingPage{}, ctx.Err()
			}
		}
		base := d.providerBaseURL(sourceHongguo)
		address := base + "/rank/" + board.path
		if page > 1 {
			address += "?page=" + strconv.Itoa(page)
		}
		requestCtx := context.WithValue(ctx, providerTextSingleAttemptKey{}, true)
		if attempt > 0 {
			requestCtx = context.WithValue(requestCtx, providerTextNoCacheKey{}, true)
		}
		body, err := d.fetchProviderText(requestCtx, address, base+"/")
		if err != nil {
			return rankingPage{}, err
		}
		result, err := parseHongguoRanking(body, board, page)
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, errHongguoRankingIncomplete) {
			return rankingPage{}, err
		}
		lastErr = err
	}
	return rankingPage{}, lastErr
}

func hongguoRankingURLPage(address string, board rankingBoard) (int, bool) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.User != nil || strings.TrimRight(parsed.Path, "/") != "/rank/"+board.path {
		return 0, false
	}
	if parsed.Host != "" && parsed.Hostname() != "hongguoduanju.com" && parsed.Hostname() != "www.hongguoduanju.com" {
		return 0, false
	}
	value := parsed.Query().Get("page")
	if value == "" {
		return 1, true
	}
	page, err := strconv.Atoi(value)
	return page, err == nil && page >= 1 && page <= 500
}

func hongguoRankingDramaID(address string) string {
	parsed, err := url.Parse(address)
	if err != nil || parsed.User != nil || parsed.Path != "/detail" {
		return ""
	}
	if parsed.Host != "" && parsed.Hostname() != "hongguoduanju.com" && parsed.Hostname() != "www.hongguoduanju.com" {
		return ""
	}
	id := parsed.Query().Get("series_id")
	if !hongguoNumericID.MatchString(id) {
		return ""
	}
	return id
}

func parseHongguoRankingDocument(body string, board rankingBoard, page int, loader map[string]any) (rankingPage, error) {
	if len(body) > 4<<20 {
		return rankingPage{}, errHongguoRankingFormat
	}
	identified := len(loader) > 0
	for _, tag := range hongguoRankingCanonical.FindAllString(body, -1) {
		if extractAttr(tag, "rel") != "canonical" {
			continue
		}
		actual, valid := hongguoRankingURLPage(extractAttr(tag, "href"), board)
		if !valid || actual != page {
			return rankingPage{}, errHongguoRankingFormat
		}
		identified = true
	}
	if !identified {
		return rankingPage{}, errHongguoRankingIncomplete
	}
	pagination := hongguoRankingPagination.FindAllStringSubmatch(body, -1)
	if len(pagination) != 1 {
		return rankingPage{}, errHongguoRankingIncomplete
	}
	current := hongguoRankingCurrentPage.FindAllStringSubmatch(pagination[0][1], -1)
	if len(current) != 1 || rankingHTMLText(current[0][1]) != strconv.Itoa(page) {
		return rankingPage{}, errHongguoRankingFormat
	}
	result := rankingPage{Items: []rankingItem{}, UpdatedText: mapString(loader, "updatedText")}
	maximumPage := page
	for _, match := range hongguoRankingAnchor.FindAllStringSubmatch(pagination[0][1], -1) {
		actual, valid := hongguoRankingURLPage(extractAttr(match[0], "href"), board)
		if !valid {
			return rankingPage{}, errHongguoRankingFormat
		}
		if actual > maximumPage {
			maximumPage = actual
		}
		if extractAttr(match[0], "rel") == "next" {
			if actual != page+1 {
				return rankingPage{}, errHongguoRankingFormat
			}
			result.HasMore = true
		}
	}
	if !result.HasMore {
		if maximumPage != page {
			return rankingPage{}, errHongguoRankingFormat
		}
		result.TotalPages = page
	}
	if items, found := parseHongguoRankingJSONLD(body, board, page); found {
		result.Items = items
	} else {
		articles := hongguoRankingArticle.FindAllString(body, -1)
		if len(articles) > 20 {
			return rankingPage{}, errHongguoRankingFormat
		}
		for _, article := range articles {
			item, valid := parseHongguoRankingArticle(article)
			if !valid {
				return rankingPage{}, errHongguoRankingIncomplete
			}
			result.Items = append(result.Items, item)
		}
	}
	if len(result.Items) == 0 {
		return rankingPage{}, errHongguoRankingIncomplete
	}
	seen, previous := map[string]bool{}, (page-1)*20
	for _, item := range result.Items {
		if !hongguoNumericID.MatchString(item.Drama.SourceID) || item.Drama.Title == "" || item.Rank <= previous || item.Rank > page*20 || seen[item.Drama.SourceID] {
			return rankingPage{}, errHongguoRankingFormat
		}
		seen[item.Drama.SourceID], previous = true, item.Rank
	}
	if result.HasMore && len(result.Items) != 20 {
		return rankingPage{}, errHongguoRankingIncomplete
	}
	return result, nil
}

func parseHongguoRankingJSONLD(body string, board rankingBoard, page int) ([]rankingItem, bool) {
	for _, match := range rankingJSONLD.FindAllStringSubmatch(body, -1) {
		var value struct {
			Type  string `json:"@type"`
			URL   string `json:"url"`
			Items []struct {
				Position int    `json:"position"`
				Name     string `json:"name"`
				URL      string `json:"url"`
			} `json:"itemListElement"`
		}
		if json.Unmarshal([]byte(match[1]), &value) != nil || value.Type != "ItemList" {
			continue
		}
		actual, valid := hongguoRankingURLPage(value.URL, board)
		if !valid || actual != page || len(value.Items) == 0 || len(value.Items) > 20 {
			continue
		}
		items := make([]rankingItem, 0, len(value.Items))
		for _, row := range value.Items {
			id := hongguoRankingDramaID(row.URL)
			title := strings.TrimSpace(row.Name)
			items = append(items, rankingItem{Rank: row.Position, Drama: Drama{
				ID: providerDramaID(sourceHongguo, id), Source: sourceHongguo, SourceID: id,
				Title: title, Name: title, ChannelName: "红果",
			}})
		}
		return items, true
	}
	return nil, false
}

func parseHongguoRankingArticle(article string) (rankingItem, bool) {
	openEnd := strings.Index(article, ">")
	if openEnd < 0 {
		return rankingItem{}, false
	}
	label := extractAttr(article[:openEnd+1], "aria-labelledby")
	id := strings.TrimPrefix(label, "rank-title-")
	if !hongguoNumericID.MatchString(id) {
		return rankingItem{}, false
	}
	var title string
	for _, heading := range hongguoRankingHeading.FindAllStringSubmatch(article, -1) {
		if heading[1] == label {
			title = rankingHTMLText(heading[2])
			break
		}
	}
	if title == "" || len([]rune(title)) > 200 {
		return rankingItem{}, false
	}
	drama := Drama{ID: providerDramaID(sourceHongguo, id), Source: sourceHongguo, SourceID: id, Title: title, Name: title, ChannelName: "红果"}
	rank := 0
	for _, link := range hongguoRankingAnchor.FindAllStringSubmatch(article, -1) {
		if !strings.HasPrefix(extractAttr(link[0], "aria-label"), "查看") {
			continue
		}
		if hongguoRankingDramaID(extractAttr(link[0], "href")) != id {
			return rankingItem{}, false
		}
		value, err := strconv.Atoi(rankingHTMLText(link[1]))
		if err == nil && value > 0 && (rank == 0 || rank == value) {
			rank = value
		}
	}
	for _, value := range strings.Fields(rankingHTMLText(article)) {
		text := strings.TrimSpace(value)
		if hongguoRankingMetric.MatchString(text) {
			drama.Heat = text
		}
		if strings.HasPrefix(text, "评分") {
			drama.Score = strings.TrimSpace(strings.TrimPrefix(text, "评分"))
		}
	}
	for _, paragraph := range hongguoRankingParagraph.FindAllStringSubmatch(article, -1) {
		var tags []string
		for _, span := range hongguoRankingSpan.FindAllStringSubmatch(paragraph[1], -1) {
			value := rankingHTMLText(span[1])
			if value != "" && len([]rune(value)) <= 20 && !strings.Contains(value, "热度") {
				tags = append(tags, value)
			}
		}
		if len(tags) > 0 {
			drama.Tags, drama.CategoryName = tags, tags[0]
		} else {
			text := rankingHTMLText(paragraph[1])
			if text != "" {
				drama.Desc = text
				drama.Intro = text
			}
		}
	}
	return rankingItem{Rank: rank, Drama: drama, Metric: drama.Heat}, rank > 0
}

func rankingHTMLText(value string) string {
	return strings.Join(strings.Fields(html.UnescapeString(hongguoRankingTags.ReplaceAllString(value, " "))), " ")
}
