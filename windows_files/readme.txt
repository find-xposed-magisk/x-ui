we don't have bash menu for windows
if you forgot your password you need to check your database with https://sqlitebrowser.org/
the app need to be open all the time

default setting:
http://localhost:54321/
user: admin
pass: admin
port: 54321


openssl req -x509 -nodes -days 365 -newkey rsa:2048 -keyout localhost.key -out localhost.crt
