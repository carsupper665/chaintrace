package model

import (
	"chaintrace/model/store"
	"errors"
)

var (
	ErrUserRequire = errors.New("user require")
)

func GetUserByUsername(username string) (user *store.User, err error) {
	user = &store.User{}

	err = DB.Where("username = ?", username).First(user).Error
	if err != nil {
		return nil, err
	}

	return user, nil
}

func GetUserByEmail(email string) (user *store.User, err error) {
	user = &store.User{}

	err = DB.Where("email = ?", email).First(user).Error
	if err != nil {
		return nil, err
	}

	return user, nil
}

func AddUser(user *store.User) error {
	if user == nil {
		return ErrUserRequire
	}

	return DB.Create(user).Error
}

func UpdateUser(user *store.User) error {
	if user == nil {
		return ErrUserRequire
	}

	if user.ID == 0 {
		return errors.New("user id is required")
	}

	return DB.Save(user).Error
}

func DeleteUser(user *store.User) error {
	if user == nil {
		return ErrUserRequire
	}

	if user.ID == 0 {
		return errors.New("user id is required")
	}

	return DB.Delete(user).Error
}

func IsExists(username string) bool {
	user := &store.User{}
	err := DB.Where("username = ?", username).First(user).Error
	if err != nil {
		return false
	}
	return true
}

func IsEmailExist(email string) bool {
	user := &store.User{}
	err := DB.Where("email = ?", email).First(user).Error
	if err != nil {
		return false
	}
	return true
}
