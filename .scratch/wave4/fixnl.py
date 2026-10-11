import re
p = 'reference/native/tools/measure/main.go'
data = open(p, 'rb').read()
# Replace every broken literal-newline Split string with the escaped form.
broken = b'strings.Split(string(lockBytes), "\n")'
fixed = b'strings.Split(string(lockBytes), "\\n")'
n = data.count(broken)
data = data.replace(broken, fixed)
open(p, 'wb').write(data)
print('replaced', n)
# verify
data = open(p, 'rb').read()
print('broken remaining:', data.count(broken))
print('fixed present:', data.count(fixed))
