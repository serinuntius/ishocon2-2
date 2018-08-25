package main

import (
	"context"
)

// Candidate Model
type Candidate struct {
	ID             int
	Name           string
	PoliticalParty string
	Sex            string
	VotedCount      int
}

// CandidateElectionResult type
type CandidateElectionResult struct {
	ID             int
	Name           string
	PoliticalParty string
	Sex            string
	VotedCount      int
}

// PartyElectionResult type
type PartyElectionResult struct {
	PoliticalParty string
	VoteCount      int
}

func getAllCandidate(ctx context.Context) (candidates []Candidate) {
	rows, err := db.QueryContext(ctx, "SELECT * FROM candidates")
	if err != nil {
		panic(err.Error())
	}
	defer rows.Close()

	for rows.Next() {
		c := Candidate{}
		err = rows.Scan(&c.ID, &c.Name, &c.PoliticalParty, &c.Sex, &c.VotedCount)
		if err != nil {
			panic(err.Error())
		}
		candidates = append(candidates, c)
	}

	return
}

func getCandidate(ctx context.Context, candidateID int) (c Candidate, err error) {
	row := db.QueryRowContext(ctx, "SELECT * FROM candidates WHERE id = ?", candidateID)
	err = row.Scan(&c.ID, &c.Name, &c.PoliticalParty, &c.Sex, &c.VotedCount)

	return
}

func getCandidatesByPoliticalParty(ctx context.Context, party string) (candidates []Candidate) {
	rows, err := db.QueryContext(ctx, "SELECT * FROM candidates WHERE political_party = ?", party)
	if err != nil {
		panic(err.Error())
	}
	defer rows.Close()

	for rows.Next() {
		c := Candidate{}
		err = rows.Scan(&c.ID, &c.Name, &c.PoliticalParty, &c.Sex, &c.VotedCount)
		if err != nil {
			panic(err.Error())
		}
		candidates = append(candidates, c)
	}

	return
}

func getElectionResult(ctx context.Context) (result []CandidateElectionResult) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.id, c.name, c.political_party, c.sex, voted_count
		FROM candidates AS c
		ORDER BY voted_count DESC`)
	if err != nil {
		panic(err.Error())
	}
	defer rows.Close()

	for rows.Next() {
		r := CandidateElectionResult{}
		err = rows.Scan(&r.ID, &r.Name, &r.PoliticalParty, &r.Sex, &r.VotedCount)
		if err != nil {
			panic(err.Error())
		}
		result = append(result, r)
	}

	return
}
