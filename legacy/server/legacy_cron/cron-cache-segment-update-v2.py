# -*- coding: utf-8 -*-
"""
Created on Tue Nov 14 14:34:28 2023

@author: Admin

2023-11-29: cache-segment-update-v1: update từ 1 page nào đó trở đi:
https://diaocthongthai.com/en/map-of-us-cities-and-places/page/1755/

2023-12-02: v2 - cho phép điền nhiều danh mục 1 lúc.
Bổ sung thêm option 3. Chỉ update bài viết, bỏ qua archive

#cron-cache-segment-update-v2
Sử dụng từ file cache-segment-update-v2

"""

import os
import psutil
import requests
from bs4 import BeautifulSoup as bs
import re
import time

time_now = time.strftime('%Y-%m-%d_%H-%M-%S')

def logging(*args):
    current_dir = os.path.dirname(os.path.realpath(__file__))
    log_message = ' '.join(map(str, args))
    with open(f"{current_dir}/log/{time_now}_cache_clean_segment.log", "a+", encoding="utf-8") as f:
        f.write(log_message + "\n")
    print(log_message)

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
    
    if directory[-1:] == '/':
        directory = directory[:-1]

    return user, domain, directory

# Function to trigger cache creation with curl
def trigger_cache_creation(url):
    logging(f'curl {url} > /dev/null 2>&1')
    os.system(f'curl -A "Mozilla/5.0 (iPhone; CPU iPhone OS 10_3 like Mac OS X) AppleWebKit/602.1.50 (KHTML, like Gecko) Version/10.0 Mobile/14E5239e Safari/602.1" {url} > /dev/null 2>&1')
    time.sleep(2)
    os.system(f'curl {url} > /dev/null 2>&1')

# Function to check CPU usage and wait if it's too high
def check_cpu_usage_and_wait(threshold=70):
    while True:
        cpu_percent = psutil.cpu_percent(interval=1)
        if cpu_percent <= threshold:
            break
        logging ("High CPU. Sleep in 10 seconds")
        time.sleep(10)

def delete_cache(url):
    _, _, directory = get_user_and_directory(url)
    
    # Remove Cache Files
    cache_file_html = f'/home/{user}/{domain}/public_html/wp-content/cache/wp-rocket/{domain}/{directory}/index-https.html'
    cache_file_gzip = f'/home/{user}/{domain}/public_html/wp-content/cache/wp-rocket/{domain}/{directory}/index-https.html_gzip'
    mobile_cache_file_html = f'/home/{user}/{domain}/public_html/wp-content/cache/wp-rocket/{domain}/{directory}/index-mobile-https.html'
    mobile_cache_file_gzip = f'/home/{user}/{domain}/public_html/wp-content/cache/wp-rocket/{domain}/{directory}/index-mobile-https.html_gzip'
    
    if os.path.exists(cache_file_html):
        os.remove(cache_file_html)
        logging(f'Deleted files: {directory}/index-https.html')
        
    if os.path.exists(cache_file_gzip):
        os.remove(cache_file_gzip)
        logging(f'Deleted files: {directory}/index-https.html_gzip')

    if os.path.exists(mobile_cache_file_html):
        os.remove(mobile_cache_file_html)
        logging(f'Deleted files: {directory}/index-mobile-https.html')

    if os.path.exists(mobile_cache_file_gzip):
        os.remove(mobile_cache_file_gzip)
        logging(f'Deleted files: {directory}/index-mobile-https.html_gzip')        
        
headers = {'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36'}
mobile_headers = {'User-Agent': 'Mozilla/5.0 (iPhone; CPU iPhone OS 10_3 like Mac OS X) AppleWebKit/602.1.50 (KHTML, like Gecko) Version/10.0 Mobile/14E5239e Safari/602.1'}


USER_DIR = '/var/hostvn/users'


def tien_hanh_update_cache(my_archive_link_list, selected_option):
    for archive_link in my_archive_link_list.split(','):

        if archive_link[-1:] != '/':
            archive_link = archive_link + '/'
        
        archive_pages = [archive_link]
        
        if '/page/' in archive_link:
            page_index = archive_link.find('/page/') + len('/page/')
            first_page_number = archive_link[page_index:].split('/')[0]
            current_page = int(first_page_number)
            root_link = archive_link.split('/page/')[0]
        
        else:
            current_page = 1
            root_link = archive_link[:-1]
            
        global user, domain, current_directory
        user, domain, current_directory = get_user_and_directory(archive_link)
        
        current_cache_file_html = f'/home/{user}/{domain}/public_html/wp-content/cache/wp-rocket/{domain}/{current_directory}/index-https.html'
        current_cache_file_gzip = f'/home/{user}/{domain}/public_html/wp-content/cache/wp-rocket/{domain}/{current_directory}/index-https.html_gzip'
        current_mobile_cache_file_html = f'/home/{user}/{domain}/public_html/wp-content/cache/wp-rocket/{domain}/{current_directory}/index-mobile-https.html'
        current_mobile_cache_file_gzip = f'/home/{user}/{domain}/public_html/wp-content/cache/wp-rocket/{domain}/{current_directory}/index-mobile-https.html_gzip'
        mobile_active_file = f'/home/{user}/{domain}/public_html/wp-content/cache/wp-rocket/{domain}/{current_directory}/.mobile-active'
        
        if os.path.exists(current_cache_file_html):
            os.remove(current_cache_file_html)
            logging(f'Deleted files: {current_directory}/index-https.html')
            
        if os.path.exists(current_cache_file_gzip):
            os.remove(current_cache_file_gzip)
            logging(f'Deleted files: {current_directory}/index-https.html_gzip')
            
        if os.path.exists(current_mobile_cache_file_html):
            os.remove(current_mobile_cache_file_html)
            logging(f'Deleted files: {current_directory}/index-mobile-https.html')
            
        if os.path.exists(current_mobile_cache_file_gzip):
            os.remove(current_mobile_cache_file_gzip)
            logging(f'Deleted files: {current_directory}/index-mobile-https.html_gzip')

        if os.path.exists(mobile_active_file):
            os.remove(mobile_active_file)
            logging(f'Deleted files: {current_directory}/.mobile-active')

        # Detect page_number from nav_below section in archive_link
        page = requests.get(archive_link, headers=headers)
        time.sleep(2)
        mobile_page = requests.get(archive_link, headers=mobile_headers)

        soup = bs(page.content, 'lxml')
        nav_below = soup.find(id="nav-below")
        
        if nav_below == None:
            pass
                
        else:
            next_item = nav_below.find("a", class_="next")
            last_nav = next_item.find_previous_sibling()["href"]
            
            match = re.search(r'/page/(\d+)/$', last_nav)
            
            if match:
                page_number = match.group(1)
        
        # Get full list url of archive pages     
        for page_num in range(current_page+1, int(page_number) + 1):
            
            page_url = root_link + '/page/' + str(page_num) + '/'
            
            archive_pages.append(page_url)
            
        # Update cache for each archive page
        for archive_page in archive_pages:
            logging(archive_page)
            
            if selected_option == '3':
                pass
            
            else:
                delete_cache(archive_page)
            
            check_cpu_usage_and_wait(threshold=70)
            
            # Crawl Post Link and update cache
            try:
                page = requests.get(archive_page, headers=headers)
                time.sleep(1)
                mobile_page = requests.get(archive_page, headers=mobile_headers)
                time.sleep(1)
            except:
                time.sleep(300)
                page = requests.get(archive_page, headers=headers)
                time.sleep(1)        
                mobile_page = requests.get(archive_page, headers=mobile_headers)
                time.sleep(1)
                        
            if selected_option == '1':
                logging("===")
                continue
        
            else:
        
                soup = bs(page.content, 'lxml')
                
                articles = soup.find(id='main').find_all('article')
                
                if articles == []:
                    #logging(soup)
                    logging("No article found")
                    continue
                
                for article in articles:
                    url = article.find('a')['href']
                    logging(url)
                    
                    delete_cache(url)
                    
                    check_cpu_usage_and_wait(threshold=70)
                    
                    trigger_cache_creation(url)
                    
                    logging('---')
                    time.sleep(2.5)
                
                logging("====")
        
        
# my_archive_link_list
# Let input archive/category link 
# Split by comma for many link
# archive_link = "https://diaocthongthai.com/quan-huyen-viet-nam/"


# Variable selected_option
# Select one of the below update cache options:
#     1. Update Archive only                      
#     2. Update both Archive and its Posts, too.
#     3. Update Posts only.


tien_hanh_update_cache("https://diaocthongthai.com/tags/the-emerald-68/,https://diaocthongthai.com/tags/lumiere-boulevard/,https://diaocthongthai.com/tags/glory-heights/,https://diaocthongthai.com/tags/lovera-vista/","3")

tien_hanh_update_cache("https://diaocthongthai.com/ban-tin-du-an/","1")

tien_hanh_update_cache("https://diaocthongthai.com/cac-thanh-pho-cua-my/","1")