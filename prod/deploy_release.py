#!/usr/bin/python3
# SPDX-FileCopyrightText: 2022 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
#
# Script for deploying a release on Wikimedia Toolforge.
#
# Usage:
#
# $ ssh bastion.toolforge.org
# $ become osmviews
# $ python3 prod/deploy_release.py

import json
import os
import pathlib
import shutil
import urllib.request


def deploy_release(tag='latest'):
    path = download_release(tag)
    restart_webserver(path)


def download_release(tag):
    url = 'https://api.github.com/repos/brawer/osmviews/releases/'
    if tag is None or tag is 'latest':
        url += 'latest'
    else:
        url += 'tags/' + tag
    with urllib.request.urlopen(url) as response:
        info = json.loads(response.read())
    tag = info['tag_name']  # 'latest' -> actual tag
    assets = info['assets']
    print('Deploying release', tag)
    path = pathlib.Path('bin', tag)
    tmp_path = pathlib.Path('bin', 'tmp-%s.tmp' % tag)
    shutil.rmtree(tmp_path, ignore_errors=True)
    tmp_path.mkdir(parents=True)
    for asset in assets:
        with urllib.request.urlopen(asset['browser_download_url']) as response:
            filepath = pathlib.PurePath(tmp_path, asset['name'])
            with open(filepath, 'wb') as f:
                shutil.copyfileobj(response, f)
            os.chmod(filepath, 0o0755)
    shutil.rmtree(path, ignore_errors=True)
    tmp_path.rename(path)
    return path


def restart_webserver(path):
    binary = (path / 'webserver').resolve()
    cmd = ['webserver', '--backend=kubernetes', 'golang',
           'restart', str(binary)]
    print(' '.join(cmd))


if __name__ == '__main__':
    deploy_release(tag='latest')
