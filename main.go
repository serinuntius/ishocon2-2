package main

import (
	"bytes"
	"database/sql"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/gin-contrib/cache"
	"github.com/gin-contrib/cache/persistence"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis"
	_ "github.com/go-sql-driver/mysql"
	"github.com/pkg/errors"
	"github.com/serinuntius/graqt"
)

var (
	db           *sql.DB
	rc           *redis.Client
	traceEnabled = os.Getenv("GRAQT_TRACE")
	driverName   = "mysql"
	candidates   []Candidate
)

func NewRedisClient() error {
	rc = redis.NewClient(&redis.Options{
		Network:  "unix",
		Addr:     "/var/run/redis/redis-server.sock",
		Password: "",
		DB:       0,
	})

	_, err := rc.Ping().Result()
	if err != nil {
		return err
	}
	return nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func main() {
	// database setting
	if traceEnabled == "1" {
		driverName = "mysql-tracer"
		graqt.SetRequestLogger("log/request.log")
		graqt.SetQueryLogger("log/query.log")
	}

	user := getEnv("ISHOCON2_DB_USER", "ishocon")
	pass := getEnv("ISHOCON2_DB_PASSWORD", "ishocon")
	dbname := getEnv("ISHOCON2_DB_NAME", "ishocon2")
	db, _ = sql.Open(driverName, user+":"+pass+"@/"+dbname)
	db.SetMaxIdleConns(5)

	// redis
	if err := NewRedisClient(); err != nil {
		log.Fatal(err)
	}

	gin.SetMode(gin.DebugMode)
	r := gin.Default()
	if traceEnabled == "1" {
		r.Use(graqt.RequestIdForGin())
	}
	pprof.Register(r)

	// add cache store(not redis)
	store := persistence.NewInMemoryStore(time.Minute)

	r.FuncMap = template.FuncMap{"indexPlus1": func(i int) int { return i + 1 }}

	r.LoadHTMLGlob("templates/*.tmpl")

	// GET /
	r.GET("/", cache.CachePage(store, time.Minute, func(c *gin.Context) {
		electionResults := getElectionResult(c)

		// 上位10人と最下位のみ表示
		tmp := make([]CandidateElectionResult, len(electionResults))
		copy(tmp, electionResults)
		candidates := tmp[:10]
		candidates = append(candidates, tmp[len(tmp)-1])

		partyNames := getAllPartyName(c)
		partyResultMap := map[string]int{}
		for _, name := range partyNames {
			partyResultMap[name] = 0
		}
		for _, r := range electionResults {
			partyResultMap[r.PoliticalParty] += r.VoteCount
		}
		partyResults := []PartyElectionResult{}
		for name, count := range partyResultMap {
			r := PartyElectionResult{}
			r.PoliticalParty = name
			r.VoteCount = count
			partyResults = append(partyResults, r)
		}
		// 投票数でソート
		sort.Slice(partyResults, func(i, j int) bool { return partyResults[i].VoteCount > partyResults[j].VoteCount })

		sexRatio := map[string]int{
			"men":   0,
			"women": 0,
		}
		for _, r := range electionResults {
			if r.Sex == "男" {
				sexRatio["men"] += r.VoteCount
			} else if r.Sex == "女" {
				sexRatio["women"] += r.VoteCount
			}
		}

		c.HTML(http.StatusOK, "index.tmpl", gin.H{
			"candidates": candidates,
			"parties":    partyResults,
			"sexRatio":   sexRatio,
		})
	}))

	// GET /candidates/:candidateID(int)
	r.GET("/candidates/:candidateID", cache.CachePage(store, time.Minute, func(c *gin.Context) {
		candidateID, _ := strconv.Atoi(c.Param("candidateID"))
		candidate, err := getCandidate(c, candidateID)
		if err != nil {
			c.Redirect(http.StatusFound, "/")
		}
		votes := getVoteCountByCandidateID(c, candidateID)
		candidateIDs := []int{candidateID}
		keywords := getVoiceOfSupporter(c, candidateIDs)

		c.HTML(http.StatusOK, "candidate.tmpl", gin.H{
			"candidate": candidate,
			"votes":     votes,
			"keywords":  keywords,
		})
	}))

	// GET /political_parties/:name(string)
	r.GET("/political_parties/:name", cache.CachePage(store, time.Minute, func(c *gin.Context) {
		partyName := c.Param("name")
		var votes int
		electionResults := getElectionResult(c)
		for _, r := range electionResults {
			if r.PoliticalParty == partyName {
				votes += r.VoteCount
			}
		}

		candidates := getCandidatesByPoliticalParty(c, partyName)
		candidateIDs := []int{}
		for _, c := range candidates {
			candidateIDs = append(candidateIDs, c.ID)
		}
		keywords := getVoiceOfSupporter(c, candidateIDs)

		c.HTML(http.StatusOK, "political_party.tmpl", gin.H{
			"politicalParty": partyName,
			"votes":          votes,
			"candidates":     candidates,
			"keywords":       keywords,
		})
	}))

	// GET /vote
	r.GET("/vote", func(c *gin.Context) {
		c.HTML(http.StatusOK, "vote.tmpl", gin.H{
			"candidates": candidates,
			"message":    "",
		})
	})

	// POST /vote
	r.POST("/vote", func(c *gin.Context) {
		user, userErr := getUser(c, c.PostForm("name"), c.PostForm("address"), c.PostForm("mynumber"))
		candidate, cndErr := getCandidateByName(c, c.PostForm("candidate"))
		votedCount := getUserVotedCount(c, user.ID)
		voteCount, _ := strconv.Atoi(c.PostForm("vote_count"))

		if userErr != nil {
			if err := voteErrorCache(c,"個人情報に誤りがあります"); err != nil {
				log.Fatal(err)
			}
		} else if user.Votes < voteCount+votedCount {
			if err := voteErrorCache(c,"投票数が上限を超えています"); err != nil {
				log.Fatal(err)
			}
		} else if c.PostForm("candidate") == "" {
			if err := voteErrorCache(c,"候補者を記入してください"); err != nil {
				log.Fatal(err)
			}
		} else if cndErr != nil {
			if err := voteErrorCache(c,"候補者を正しく記入してください"); err != nil {
				log.Fatal(err)
			}
		} else if c.PostForm("keyword") == "" {
			if err := voteErrorCache(c,"投票理由を記入してください"); err != nil {
				log.Fatal(err)
			}
		} else {
			for i := 1; i <= voteCount; i++ {
				createVote(c, user.ID, candidate.ID, c.PostForm("keyword"))
			}
		}

		// postが終わるたびに消す
		store.Flush()

		if err := voteCache(c); err != nil {
			log.Fatal(err)
		}
	})

	r.GET("/initialize", func(c *gin.Context) {
		db.Exec("DELETE FROM votes")

		// initでもcache消す
		store.Flush()
		rc.FlushAll()

		candidates = getAllCandidate(c)

		c.String(http.StatusOK, "Finish")
	})

	r.Run(":8080")
}

const voteCacheKey = "voteCache"

func voteCache(c *gin.Context) error {
	cachedBytes, err := rc.Get(voteCacheKey).Bytes()
	if err != nil && err != redis.Nil {
		return errors.Wrap(err, "Failed to rc.Get")
	} else if err == redis.Nil {
		// cacheがないので普通に返す
		bcw := &bodyCacheWriter{
			body:           &bytes.Buffer{},
			ResponseWriter: c.Writer,
		}
		c.Writer = bcw

		c.HTML(http.StatusOK, "vote.tmpl", gin.H{
			"candidates": candidates,
			"message":    "投票に成功しました",
		})

		bs, err := ioutil.ReadAll(bcw.body)
		if err != nil {
			return errors.Wrap(err, "Failed to ReadAll")
		}

		if _, err := rc.Set(voteCacheKey, bs, time.Minute).Result(); err != nil {
			return errors.Wrap(err, "Failed to rc.Set")
		}
	}

	// cacheがあるのでそれを返す
	c.Writer.Write(cachedBytes)
	return nil
}

func voteErrorCache(c *gin.Context, msg string) error {
	cachedBytes, err := rc.Get(msg).Bytes()
	if err != nil && err != redis.Nil {
		return errors.Wrap(err, "Failed to rc.Get")
	} else if err == redis.Nil {
		// cacheがないので普通に返す
		bcw := &bodyCacheWriter{
			body:           &bytes.Buffer{},
			ResponseWriter: c.Writer,
		}
		c.Writer = bcw

		c.HTML(http.StatusOK, "vote.tmpl", gin.H{
			"message": msg,
		})

		bs, err := ioutil.ReadAll(bcw.body)
		if err != nil {
			return errors.Wrap(err, "Failed to ReadAll")
		}

		if _, err := rc.Set(msg, bs, time.Minute).Result(); err != nil {
			return errors.Wrap(err, "Failed to rc.Set")
		}
	}

	// cacheがあるのでそれを返す
	c.Writer.Write(cachedBytes)
	return nil
}

type bodyCacheWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w bodyCacheWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}
