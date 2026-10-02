package main

import "github.com/Dr0nj/regente-agent/security"

var executionPolicy = new(string)
var jobSecrets = new(string)

func childEnvironment() []string { return security.CleanEnvironment() }
