# PHP with Laravel's standalone packages preloaded, so a single file can use
# collect(), Str::, Arr:: and Carbon without a full Laravel app. Every file run
# here gets the autoloader prepended, so no require line is needed.
FROM php:8.3-cli-alpine

COPY --from=composer:2 /usr/bin/composer /usr/bin/composer

RUN mkdir -p /opt/laravel && cd /opt/laravel && \
    composer require --no-interaction --no-progress --prefer-dist \
        illuminate/collections:^11.0 illuminate/support:^11.0 nesbot/carbon:^3.0 && \
    composer clear-cache && rm /usr/bin/composer && \
    echo 'auto_prepend_file=/opt/laravel/prepend.php' > /usr/local/etc/php/conf.d/laravel.ini && \
    printf '%s\n' '<?php' 'require "/opt/laravel/vendor/autoload.php";' \
        'class_alias(Illuminate\Support\Str::class, "Str");' \
        'class_alias(Illuminate\Support\Arr::class, "Arr");' \
        'class_alias(Illuminate\Support\Collection::class, "Collection");' \
        'class_alias(Carbon\Carbon::class, "Carbon");' > /opt/laravel/prepend.php

RUN addgroup -S modigo && adduser -S modigo -G modigo && \
    mkdir -p /code && chown modigo:modigo /code

WORKDIR /code
USER modigo

CMD ["php", "index.php"]
