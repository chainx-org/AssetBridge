#!/usr/bin/expect -f

set timeout 30

spawn ./build/bbridge --config ./relayers/config_Yyy.json

expect "*>"
send "111111\r"

expect "*>"
send "111111\r"

expect "*>"
send "111111\r"

expect "*>"
send "111\r"

interact
