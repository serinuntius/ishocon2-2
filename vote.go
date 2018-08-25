package main

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

// Vote Model
type Vote struct {
	ID          int
	UserID      int
	CandidateID int
	Keyword     string
	VotedCount  int
}

func createVote(ctx context.Context, userID int, candidateID int, keyword string, voteCount int) error {
	politicalParty := candidateIdMap[candidateID].PoliticalParty

	var eg errgroup.Group

	eg.Go(func() error {
		_, err := rc.ZIncrBy(politicalParty, float64(voteCount), keyword).Result()
		if err != nil {
			return errors.Wrap(err, "")
		}
		return nil
	})

	eg.Go(func() error {
		_, err := rc.ZIncrBy(candidateZKey(candidateID), float64(voteCount), keyword).Result()
		if err != nil {
			return errors.Wrap(err, "")
		}
		return nil
	})

	eg.Go(func() error {
		_, err := rc.ZIncrBy(kojinKey(), float64(voteCount), candidateVotedCountKey(candidateID)).Result()
		if err != nil {
			return errors.Wrap(err, "")
		}
		return nil
	})

	eg.Go(func() error {
		_, err := incrStore.Increment(candidateKey(candidateID), uint64(voteCount))
		if err != nil {
			return errors.Wrap(err, "")
		}
		return nil
	})

	eg.Go(func() error {
		_, err := incrStore.Increment(userKey(userID), uint64(voteCount))
		if err != nil {
			return errors.Wrap(err, "")
		}
		return nil
	})

	eg.Go(func() error {
		sex := candidateIdMap[candidateID].Sex
		_, err := incrStore.Increment(sexKey(sex), uint64(voteCount))
		if err != nil {
			return errors.Wrap(err, "")
		}
		return nil
	})

	if err := eg.Wait(); err != nil {
		return errors.Wrap(err, "Failed to wait")
	}

	return nil
}

func getVoiceOfSupporter(candidateID int) ([]string, error) {
	voices, err := rc.ZRevRange(candidateZKey(candidateID), 0, 10).Result()
	if err != nil {
		return nil, errors.Wrap(err, "")
	}

	return voices, nil
}

func getVoiceOfSupporterByParties(politicalParty string) ([]string, error) {
	voices, err := rc.ZRevRange(politicalParty, 0, 10).Result()
	if err != nil {
		return nil, errors.Wrap(err, "")
	}

	return voices, nil
}

func candidateVotedCountKey(candidateID int) string {
	return fmt.Sprintf("candidateVotedCount:%d", candidateID)
}

func candidateZKey(candidateID int) string {
	return fmt.Sprintf("candidatez:%d", candidateID)
}

func candidateKey(candidateID int) string {
	return fmt.Sprintf("candidate:%d", candidateID)
}

func userKey(userID int) string {
	return fmt.Sprintf("user:%d", userID)
}

func kojinKey() string {
	return "kojinkey"
}

func sexKey(sex string) string {
	return "sex:" + sex
}
