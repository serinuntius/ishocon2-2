package main

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"database/sql"
	"html/template"
	"log"

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

	candidateMap   map[string]int
	candidateIdMap map[int]Candidate
)

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

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

func main() {
	if traceEnabled == "1" {
		// driverNameは絶対にこれでお願いします。
		driverName = "mysql-tracer"
		graqt.SetRequestLogger("log/request.log")
		graqt.SetQueryLogger("log/query.log")
	}

	// database setting
	user := getEnv("ISHOCON2_DB_USER", "ishocon")
	pass := getEnv("ISHOCON2_DB_PASSWORD", "ishocon")
	dbname := getEnv("ISHOCON2_DB_NAME", "ishocon2")
	var err error
	db, err = sql.Open(driverName, user+":"+pass+"@unix(/var/run/mysqld/mysqld.sock)/"+dbname)
	if err != nil {
		log.Fatal(err)
	}

	if err := NewRedisClient(); err != nil {
		log.Fatal(err)
	}

	db.SetMaxIdleConns(20)
	db.SetMaxOpenConns(40)
	db.SetConnMaxLifetime(300 * time.Second)

	//gin.SetMode(gin.DebugMode)
	//gin.SetMode(gin.ReleaseMode)

	r := gin.New()

	pprof.Register(r)

	//r.Use(static.Serve("/css", static.LocalFile("public/css", true)))
	if traceEnabled == "1" {
		r.Use(graqt.RequestIdForGin())
	}

	r.FuncMap = template.FuncMap{"indexPlus1": func(i int) int { return i + 1 }}

	r.LoadHTMLGlob("templates/*.tmpl")

	// template cache store
	store := persistence.NewInMemoryStore(time.Minute)

	// GET /
	r.GET("/", cache.CachePage(store, time.Minute, func(c *gin.Context) {
		// 1 ~ 10 の取得
		results, err := rc.ZRevRangeWithScores(kojinKey(), 0, 9).Result()
		if err != nil {
			log.Fatal(err)
		}

		// 最下位
		resultsWorst, err := rc.ZRangeWithScores(kojinKey(), 0, 0).Result()
		if err != nil {
			log.Fatal(err)
		}
		results = append(results, resultsWorst...)

		// 1 ~ 10と最下位の分枠を用意しておく
		candidateIDs := make([]int, len(results))
		votedCounts := make([]int, len(results))

		for i, r := range results {
			candidateVotedCountKey, votedCount := r.Member, r.Score
			if cKey, ok := candidateVotedCountKey.(string); ok {
				idx := strings.Index(cKey, ":")
				candidateID := cKey[idx+1:]
				candidateIDs[i], err = strconv.Atoi(candidateID)
				if err != nil {
					log.Fatal(err)
				}
				votedCounts[i] = int(votedCount)
			} else {
				log.Fatal(candidateVotedCountKey)
			}
		}

		menCount, err := rc.Get(sexKey("男")).Int64()
		if err != nil && err != redis.Nil {
			log.Fatal(err)
		} else if err == redis.Nil {
			menCount = 0
		}

		womenCount, err := rc.Get(sexKey("女")).Int64()
		if err != nil && err != redis.Nil {
			log.Fatal(err)
		} else if err == redis.Nil {
			womenCount = 0
		}

		sexRatio := map[string]int{
			"men":   int(menCount),
			"women": int(womenCount),
		}

		//partyElectionResults := [4]PartyElectionResult{}
		partyResultMap := make(map[string]int, 4)

		var cs []CandidateElectionResult

		for idx, cID := range candidateIDs {
			sex := candidateIdMap[cID].Sex
			partyName := candidateIdMap[cID].PoliticalParty
			cs = append(cs, CandidateElectionResult{
				ID:             cID,
				Name:           candidateIdMap[cID].Name,
				PoliticalParty: partyName,
				Sex:            sex,
				VotedCount:     votedCounts[idx],
			})
			partyResultMap[partyName] += votedCounts[idx]
		}

		partyResults := make([]PartyElectionResult, len(partyResultMap))
		idx := 0
		for name, count := range partyResultMap {
			r := PartyElectionResult{
				PoliticalParty: name,
				VoteCount:      count,
			}
			partyResults[idx] = r
			idx++
		}

		// 投票数でソート
		sort.Slice(partyResults, func(i, j int) bool { return partyResults[i].VoteCount > partyResults[j].VoteCount })

		c.HTML(http.StatusOK, "index.tmpl", gin.H{
			"candidates": cs,
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

		keywords, err := getVoiceOfSupporter(candidateID)
		if err != nil {
			log.Fatal(err)
		}

		votedCount, err := rc.Get(candidateKey(candidateID)).Int64()
		if err != nil && err != redis.Nil {
			log.Fatal(err)
		} else if err == redis.Nil {
			votedCount = 0
		}

		c.HTML(http.StatusOK, "candidate.tmpl", gin.H{
			"candidate": candidate,
			"votes":     votedCount,
			"keywords":  keywords,
		})
	}))

	// GET /political_parties/:name(string)
	r.GET("/political_parties/:name", cache.CachePage(store, time.Minute, func(c *gin.Context) {
		partyName := c.Param("name")

		candidates := getCandidatesByPoliticalParty(c, partyName)
		var votes int

		for _, c := range candidates {
			votedCount, err := rc.Get(candidateKey(c.ID)).Int64()
			if err != nil && err != redis.Nil {
				log.Fatal(err)
			} else if err == redis.Nil {
				votedCount = 0
			}

			votes += int(votedCount)
		}
		keywords, err := getVoiceOfSupporterByParties(partyName)
		if err != nil {
			log.Fatal(err)
		}

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
		if c.PostForm("candidate") == "" {
			if err := voteErrorCache(c, "候補者を記入してください"); err != nil {
				log.Fatal(err)
			}
			return
		} else if c.PostForm("keyword") == "" {
			if err := voteErrorCache(c, "投票理由を記入してください"); err != nil {
				log.Fatal(err)
			}
			return
		}

		candidateID, ok := candidateMap[c.PostForm("candidate")]
		if !ok {
			if err := voteErrorCache(c, "候補者を正しく記入してください"); err != nil {
				log.Fatal(err)
			}
			return
		}

		user, userErr := getUser(c, c.PostForm("name"), c.PostForm("address"), c.PostForm("mynumber"))
		if userErr != nil {
			if err := voteErrorCache(c, "個人情報に誤りがあります"); err != nil {
				log.Fatal(err)
			}
			return
		}

		voteCount, _ := strconv.Atoi(c.PostForm("vote_count"))

		userVotedCount, err := rc.Get(userKey(user.ID)).Int64()
		if err != nil && err != redis.Nil {
			log.Fatal(err)
		} else if err == redis.Nil {
			userVotedCount = 0
		}

		if user.Votes < voteCount+int(userVotedCount) {
			if err := voteErrorCache(c, "投票数が上限を超えています"); err != nil {
				log.Fatal(err)
			}
			return
		}

		go func() {
			if err := createVote(c, user.ID, candidateID, c.PostForm("keyword"), voteCount); err != nil {
				log.Fatal(err)
			}
		}()

		user.close()
		store.Flush()

		if err := voteCache(c); err != nil {
			log.Fatal(err)
		}
	})

	r.GET("/initialize", func(c *gin.Context) {
		db.Exec("DELETE FROM votes")

		rc.FlushAll()
		store.Flush()

		candidates = getAllCandidate(c)
		initialVoteCount := 0

		candidateMap = make(map[string]int, len(candidates))
		candidateIdMap = make(map[int]Candidate, len(candidates))

		for _, c := range candidates {
			candidateMap[c.Name] = c.ID
			candidateIdMap[c.ID] = c

			// 初期化でキャッシュに0で投票しておく
			_, err = rc.ZIncrBy(kojinKey(), float64(initialVoteCount), candidateVotedCountKey(c.ID)).Result()
			if err != nil {
				log.Fatal(err)
			}
		}

		c.String(http.StatusOK, "Finish")
	})

	r.RunUnix("/var/run/go/go.sock")
	//r.Run(":8080")
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
