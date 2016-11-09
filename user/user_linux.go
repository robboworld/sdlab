/*
    sdlab - STEM Lab core daemon
    Copyright (C) 2014  Dmitry Mikhirev <mikhirev@mezon.ru>

    This program is free software: you can redistribute it and/or modify
    it under the terms of the GNU General Public License as published by
    the Free Software Foundation, either version 3 of the License, or
    (at your option) any later version.

    This program is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU General Public License for more details.

    You should have received a copy of the GNU General Public License
    along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package user

import (
	"errors"
	"io/ioutil"
	"strconv"
	"strings"
)

type User struct {
	UserName string
	Pw       string
	Uid      int
	Gid      int
	Name     string
	HomeDir  string
	Shell    string
}

type Group struct {
	Name  string
	Pw    string
	Gid   int
	Users []string
}

func Lookup(name string) (user *User, err error) {
	users, err := parsePasswd()
	if err != nil {
		return nil, errors.New("error parsing passwd file")
	}
	for i := range *users {
		if (*users)[i].UserName == name {
			return &((*users)[i]), nil
		}
	}
	return nil, errors.New("user `" + name + "' not found")
}

func LookupGroup(name string) (group *Group, err error) {
	groups, err := parseGroup()
	if err != nil {
		return nil, errors.New("error parsing group file")
	}
	for i := range *groups {
		if (*groups)[i].Name == name {
			return &((*groups)[i]), nil
		}
	}
	return nil, errors.New("group `" + name + "' not found")
}

func parseGroup() (*[]Group, error) {
	raw, err := ioutil.ReadFile("/etc/group")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(raw), "\n")
	groups := make([]Group, 0, len(lines))
	for i := range lines {
		if lines[i] == "" {
			continue
		}
		g := strings.SplitN(lines[i], ":", 4)
		gid, err := strconv.ParseInt(g[2], 10, 32)
		if err != nil {
			continue
		}
		groups = append(groups,
			Group{
				g[0],
				g[1],
				int(gid),
				strings.Split(g[3], ","),
			},
		)
	}
	return &groups, nil
}

func parsePasswd() (*[]User, error) {
	raw, err := ioutil.ReadFile("/etc/passwd")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(raw), "\n")
	users := make([]User, 0, len(lines))
	for i := range lines {
		if lines[i] == "" {
			continue
		}
		u := strings.SplitN(lines[i], ":", 7)
		uid, err := strconv.ParseInt(u[2], 10, 32)
		if err != nil {
			continue
		}
		gid, err := strconv.ParseInt(u[3], 10, 32)
		if err != nil {
			continue
		}
		users = append(users,
			User{
				u[0],
				u[1],
				int(uid),
				int(gid),
				u[4],
				u[5],
				u[6],
			},
		)
	}
	return &users, nil
}
