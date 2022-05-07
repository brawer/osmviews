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


def deploy_release(tag=None):
    url = 'https://api.github.com/repos/brawer/osmviews/releases/'
    if tag is None or tag is 'latest':
        url += 'latest'
    else:
        url += 'tags/' + tag
    with urllib.request.urlopen(url) as response:
        info = json.loads(response.read())
    tag = info['tag_name']  # latest -> actual tag
    print('Deploying release', tag)
    path = pathlib.PurePath('bin', tag)
    shutil.rmtree(path, ignore_errors=True)
    pathlib.Path(path).mkdir(parents=True)
    for asset in info['assets']:
        with urllib.request.urlopen(asset['browser_download_url']) as response:
            filepath = pathlib.PurePath(path, asset['name'])
            with open(filepath, 'wb') as f:
                shutil.copyfileobj(response, f)
            os.chmod(filepath, 0o0755)


if __name__ == '__main__':
    deploy_release(tag='latest')
