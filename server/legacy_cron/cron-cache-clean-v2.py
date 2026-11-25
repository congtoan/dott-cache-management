# -*- coding: utf-8 -*-
"""
Created on Sat Nov 11 15:41:23 2023

@author: Admin

Xóa các file cache cũ quá 55 giờ dựa vào các file url_list/clean_*.txt

(root tren VPS chay `mysql` khong can mat khau)
mysql -e "USE inp5_diaocthdp4f; SELECT post_name FROM thai_posts WHERE post_content LIKE '%wpforms id%' AND post_type = 'post';"  > clean_posts_wpforms.txt

mysql -e "USE inp5_diaocthdp4f; SELECT post_name FROM thai_posts WHERE post_content LIKE '%wpforms id%' AND post_type = 'du_an';"  > clean_du_an_wpforms.txt

mysql -e "USE inp5_diaocthdp4f; SELECT post_name FROM thai_posts WHERE post_content LIKE '%wpdatatable id%' AND post_type = 'post';"  > clean_posts_wpdatatable.txt

2023-11-14 v2: Update with write log function. Set different milestone for urgent file.

"""
import os
import datetime
import time
import psutil
import glob

time_now = time.strftime('%Y-%m-%d_%H-%M-%S')

def logging(*args):
    current_dir = os.path.dirname(os.path.realpath(__file__))
    log_message = ' '.join(map(str, args))
    with open(f"{current_dir}/log/{time_now}_cache_clean.log", "a+", encoding="utf-8") as f:
        f.write(log_message + "\n")
    print(log_message)


def get_readable_time(timestamp):
    return datetime.datetime.fromtimestamp(timestamp).strftime('%Y-%m-%d %H:%M')


# Function to get the user and directory from the URL
def get_user_and_directory(url):
    domain = url.split('//')[1].split('/')[0]
    user_conf_file = f'{USER_DIR}/.{domain}.conf'
    user = None

    if os.path.exists(user_conf_file):
        with open(user_conf_file, 'r') as conf_file:
            for line in conf_file:
                if line.startswith('username='):
                    user = line.split('=')[1].strip()
                    break

    #directory = url.replace('https://', '').replace(domain, '')
    directory = url.replace('https://' + domain + '/', '')
    if directory != '':
        directory = directory[:-1] #Remove slash at the end

    return user, domain, directory

# Function to check if cache directory exists
def is_cache_expired(user, domain, directory):
    if directory =='':
        cache_path = f'/home/{user}/{domain}/public_html/wp-content/cache/wp-rocket/{domain}/'
    else:
        cache_path = f'/home/{user}/{domain}/public_html/wp-content/cache/wp-rocket/{domain}/{directory}/'
    
    # In case missing cache dir
    if not os.path.exists(cache_path):
        return True
    
    
    # In case missing cache file
    cache_file_html = os.path.join(cache_path, 'index-https.html') 
    cache_file_gzip = cache_file_html.replace \
    ('index-https.html','index-https.html_gzip')

    mobile_cache_file_html = cache_file_html.replace \
    ('index-https.html','index-mobile-https.html')

    mobile_cache_file_gzip = cache_file_html.replace \
    ('index-https.html','index-mobile-https.html_gzip')    

    if not os.path.exists(cache_file_html) or not os.path.exists(cache_file_gzip):
        if os.path.exists(cache_file_html):
            os.remove(cache_file_html)
        if os.path.exists(cache_file_gzip):
            os.remove(cache_file_gzip)
        if os.path.exists(mobile_cache_file_html):
            os.remove(mobile_cache_file_html)
        if os.path.exists(mobile_cache_file_gzip):
            os.remove(mobile_cache_file_gzip)

        return True
    
    # Checking Expire or not
    logging('Checking File:', f'{directory}/index-https.html')    
    filestamp = os.stat(cache_file_html).st_mtime 
    timestamp_str = get_readable_time(filestamp)
    
    logging('Created at:', timestamp_str)     
    
    if filestamp > milestone:
        logging ('Not expired')
        return False
    
    else:
        logging ('File expire')
        # Delete Cache file
        
        # Delete expire cache files
        try:
            os.remove(cache_file_html)
            logging(f'Deleted files: {directory}/index-https.html')
            
            os.remove(cache_file_gzip)
            logging(f'Deleted files: {directory}/index-https.html_gzip')
        except FileNotFoundError:
            logging('No cache file deleted.')
        
        try:        
            os.remove(mobile_cache_file_html)
            logging(f'Deleted files: {directory}/index-mobile-https.html')   

            os.remove(mobile_cache_file_gzip)
            logging(f'Deleted files: {directory}/index-mobile-https.html_gzip')    
        except FileNotFoundError:
            logging('No mobile cache file deleted.')

        return True


# Function to trigger cache creation with curl
def trigger_cache_creation(url):
    logging(f'curl {url} > /dev/null 2>&1')
    os.system(f'curl {url} > /dev/null 2>&1')
    time.sleep(1)
    os.system(f'curl -A "Mozilla/5.0 (iPhone; CPU iPhone OS 10_3 like Mac OS X) AppleWebKit/602.1.50 (KHTML, like Gecko) Version/10.0 Mobile/14E5239e Safari/602.1" {url} > /dev/null 2>&1')

# Function to check CPU usage and wait if it's too high
def check_cpu_usage_and_wait(threshold=70):
    while True:
        cpu_percent = psutil.cpu_percent(interval=1)
        if cpu_percent <= threshold:
            break
        logging ("High CPU. Sleep in 10 seconds")
        time.sleep(10)

logging ("Da bat dau")

# Define the path for URL files using glob pattern
# domain = os.path.basename(os.getcwd()) # wrong code
current_file_path = os.path.abspath(__file__)
domain = os.path.basename(os.path.dirname(current_file_path))
USER_DIR = '/var/hostvn/users'
url_files = sorted(glob.glob(f'/root/tool/{domain}/url_list/clean_*.txt'))

# Loop through the URL files
for url_file in url_files:
    #Get milestone
    now = time.time()
    if 'urgent' in url_file:
        milestone = now - 3600
    else: 
        milestone = now - 55*3600
    
    # Read the URLs from the file
    with open(url_file, 'r') as file:
        urls = file.read().splitlines()
        
    # Use the first URL to get user and domain (assuming they are the same for all URLs in the file)
    if urls:
        user, domain, _ = get_user_and_directory(urls[0])

    # Loop through the URLs and check/cache them
    for url in urls:
        logging(url)
        #time.sleep(3)
        if user and domain:
            _, _, directory = get_user_and_directory(url)
            if directory=='':
                logging('Trang chủ')
                pass

            if is_cache_expired(user, domain, directory):
                logging("Creating Cache..")
                check_cpu_usage_and_wait()
                # Cache directory doesn't exist, trigger cache creation
                trigger_cache_creation(url)
                logging(f'\nCache created for {url}')
                time.sleep(2.5)  # Add a 5-second delay after triggering cache creation
            else:
                logging(f'Skip for {url}')
        else:
            logging(f'Error: Unable to determine user or domain for {url}')
        
        logging("---")

        