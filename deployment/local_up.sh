#!/usr/bin/env bash

basedir=$(cd `dirname $0`; pwd)
workspace=${basedir}

echo $workspace

rm -rf ${workspace}/.local
mkdir -p ${workspace}/.local

# build greenfield
git clone https://github.com/yutianwu/greenfield.git ${workspace}/.local/greenfield
cd ${workspace}/.local/greenfield
git checkout executable
make build

# build greenfield-storage-provider
git clone https://github.com/bnb-chain/greenfield-storage-provider.git ${workspace}/.local/greenfield-storage-provider
cd ${workspace}/.local/greenfield-storage-provider
git checkout v0.2.1-test-3
make install-tools
make build

# bring up mysql container
docker pull mysql:latest
docker stop greenfield-mysql
docker rm greenfield-mysql
docker run -d --name greenfield-mysql -p 3306:3306 -e MYSQL_ROOT_PASSWORD=123456 mysql:latest

# create databases
mysql -h 127.0.0.1 -P 3306 -u root -p123456 -e 'CREATE DATABASE sp_0; CREATE DATABASE sp_1;CREATE DATABASE sp_2; CREATE DATABASE sp_3;CREATE DATABASE sp_4; CREATE DATABASE sp_5; CREATE DATABASE sp_6;'

# run greenfield
cd ${workspace}/.local/greenfield
bash ./deployment/localup/localup.sh stop
bash ./deployment/localup/localup.sh all 1 7
bash ./deployment/localup/localup.sh export_sps 1 7 > sp.json

# run greenfield-storage-provider
cd ${workspace}/.local/greenfield-storage-provider
bash ./deployment/localup/localup.sh --stop
bash ./deployment/localup/localup.sh --generate ${workspace}/.local/greenfield/sp.json root 123456 127.0.0.1:3306
bash ./deployment/localup/localup.sh --reset
bash ./deployment/localup/localup.sh --start
sleep 10
ps -ef | grep gnfd-sp | wc -l
tail -n 1000 deployment/localup/local_env/sp0/gnfd-sp.log