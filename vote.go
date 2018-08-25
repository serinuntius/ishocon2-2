package main

import "context"

// Vote Model
type Vote struct {
	ID          int
	UserID      int
	CandidateID int
	Keyword     string
}

func getVoteCountByCandidateID(ctx context.Context, candidateID int) (count int) {
	row := db.QueryRowContext(ctx, "SELECT COUNT(*) AS count FROM votes WHERE candidate_id = ?", candidateID)
	row.Scan(&count)
	return
}

func getUserVotedCount(ctx context.Context, userID int) (count int) {
	row := db.QueryRowContext(ctx, "SELECT COUNT(*) AS count FROM votes WHERE user_id =  ?", userID)
	row.Scan(&count)
	return
}

func createVote(ctx context.Context, userID int, candidateID int, keyword string) {
	db.ExecContext(ctx, "INSERT INTO votes (user_id, candidate_id, keyword) VALUES (?, ?, ?)",
		userID, candidateID, keyword)
}

func getVoiceOfSupporter(ctx context.Context, candidateIDs []int) (voices []string) {
	rows, err := db.QueryContext(ctx, `
    SELECT keyword
    FROM votes
    WHERE candidate_id IN (?)
    GROUP BY keyword
    ORDER BY COUNT(*) DESC
    LIMIT 10`)
	if err != nil {
		return nil
	}

	defer rows.Close()
	for rows.Next() {
		var keyword string
		err = rows.Scan(&keyword)
		if err != nil {
			panic(err.Error())
		}
		voices = append(voices, keyword)
	}
	return
}
