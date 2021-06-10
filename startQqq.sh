#!/usr/bin/expect -f

set timeout 30

spawn ./build/assetbridge --config ./relayers/config2.json
expect "*>"
send "111111\r"

expect "*>"
send "111111\r"

expect "*>"
send "111111\r"

expect "*>"
send "111\r"

expect "*>"
send "111\r"

interact
