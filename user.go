package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis"
	"github.com/pkg/errors"
)

// User Model
type User struct {
	ID         int
	Name       string
	Address    string
	MyNumber   string
	Votes      int
}

var userPool = sync.Pool{
	New: func() interface{} {
		return &User{}
	},
}

func newUser() *User {
	return userPool.Get().(*User)
}

func (u *User) close() {
	userPool.Put(u)
}



func (u *User) MarshalBinary() (data []byte, err error) {
	return json.Marshal(u)
}

func (u *User) UnmarshalBinary(data []byte) error {
	if err := json.Unmarshal(data, &u); err != nil {
		return errors.Wrap(err, "Failed to unmarshal")
	}
	return nil
}

func getUser(ctx context.Context, name string, address string, myNumber string) (*User, error) {
	user := *newUser()

	if err := rc.Get(myNumberKey(myNumber)).Scan(&user); err != nil && err != redis.Nil {
		return nil, errors.Wrap(err, "Failed to redis Scan")
	} else if err != redis.Nil {
		// cache exist
		if err := user.validate(name, address); err != nil {
			return nil, errors.Wrap(err, "Failed to validate")
		}
		return &user, nil
	}

	row := db.QueryRowContext(ctx, "SELECT * FROM users WHERE mynumber = ?", myNumber)
	if err := row.Scan(&user.ID, &user.Name, &user.Address, &user.MyNumber, &user.Votes); err != nil {
		return nil, errors.Wrap(err, "Failed to scan user")
	}

	if err := user.validate(name, address); err != nil {
		return nil, errors.Wrap(err, "Failed to validate")
	}

	if _, err := rc.Set(myNumberKey(myNumber), &user, time.Minute).Result(); err != nil {
		return nil, errors.Wrap(err, "Failed to Set cache")
	}

	return &user, nil
}

func myNumberKey(mynumber string) string {
	return fmt.Sprintf("myNumberKey:%s", mynumber)
}

func (u *User) validate(name, address string) error {
	if u.Name != name || u.Address != address {
		return errors.New("user not found")
	}
	return nil
}
