#!/usr/bin/expect -f

set timeout 30

spawn ./build/bbridge --config ./relayers/config_Hhh.json
expect "*>"
send "111\r"

expect "*>"
send "111\r"

expect "*>"
send "111\r"

expect "*>"
send "111\r"

expect "*>"
send "111\r"

expect "*>"
send "111\r"

expect "*>"
send "111\r"

expect "*>"
send "111\r"

interact
