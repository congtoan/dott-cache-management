R="--resolve diaocthongthai.com:443:45.77.32.177"
for b in "" "/en"; do
  for i in 1 2 3 4 5; do
    curl -sk -o /dev/null -w "$b/wp-json/wp/v2/types/post %{http_code} %{time_total}\n" $R "https://diaocthongthai.com$b/wp-json/wp/v2/types/post?djt=$RANDOM"
    sleep 0.5
  done
done
for i in 1 2 3; do
  curl -sk -o /dev/null -w "robots.txt %{http_code} %{time_total}\n" $R "https://diaocthongthai.com/robots.txt?djt=$RANDOM"
done
