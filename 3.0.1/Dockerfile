FROM centos:centos6

MAINTAINER Traun Leyden <tleyden@couchbase.com>

ENV CB_VERSION		3.0.1
ENV CB_RELEASE_URL	http://packages.couchbase.com/releases
ENV CB_PACKAGE		couchbase-server-community-3.0.1-centos6.x86_64.rpm

# Add couchbase binaries to PATH
ENV PATH $PATH:/opt/couchbase/bin:/opt/couchbase/bin/tools:/opt/couchbase/bin/install

# Install yum dependencies
RUN yum install -y \
    hostname \
    initscripts \
    openssl \
    pkgconfig \
    sudo \
    tar \
    wget 

# Install couchbase
RUN rpm --install $CB_RELEASE_URL/$CB_VERSION/$CB_PACKAGE

# Add start script
ADD scripts/couchbase-start /usr/local/bin/

