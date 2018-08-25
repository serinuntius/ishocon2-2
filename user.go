package main

import "context"

// User Model
type User struct {
	ID       int
	Name     string
	Address  string
	MyNumber string
	Votes    int
}

func getUser(ctx context.Context, name string, address string, myNumber string) (user User, err error) {
	row := db.QueryRowContext(ctx, "SELECT * FROM users WHERE name = ? AND address = ? AND mynumber = ?",
		name, address, myNumber)
	err = row.Scan(&user.ID, &user.Name, &user.Address, &user.MyNumber, &user.Votes)
	return
}
