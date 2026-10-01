package app

import "errors"

var ErrNodeRoamingBusy = errors.New("native roaming owner has active or uncertain work")
var ErrNodeRoamingPreflight = errors.New("native preparation refused before source retirement")
