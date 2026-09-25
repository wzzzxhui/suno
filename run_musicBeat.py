import requests
import datetime
import hashlib
import hmac
import base64

GMT_FORMAT = "%a, %d %b %Y %H:%M:%S GMT"
API_GATE_SOURCE = "uin_userName"
API_GATE_SECRET_ID = "apiSecretId"
API_GATE_SECRET_KEY = "apiSecretKey"
URL = "https://api.mediax.tencent.com/job"
TMP_CONTENT_ID = "tmpContentId"
TMP_SECRET_ID = "tmpSecretId"
TMP_SECRET_KEY = "tmpSecretKey"


def get_api_gate_signature(source, secret_id, secret_key):
    date_time = datetime.datetime.utcnow().strftime(GMT_FORMAT)
    auth = (
        'hmac id="'
        + secret_id
        + '", algorithm="hmac-sha1", headers="date source", signature="'
    )
    signStr = "date: " + date_time + "\n" + "source: " + source
    sign = hmac.new(secret_key.encode(), signStr.encode(), hashlib.sha1).digest()
    sign = base64.b64encode(sign).decode()
    sign = auth + sign + '"'
    return sign, date_time, source


if __name__ == "__main__":
    sign, date_time, source = get_api_gate_signature(
        API_GATE_SOURCE, API_GATE_SECRET_ID, API_GATE_SECRET_KEY
    )
    headers = {
        "Content-Type": "application/json",
        "Authorization": sign,
        "Date": date_time,
        "Source": source,
    }

    request = {
        "action": "CreateJob",
        "secretId": TMP_SECRET_ID,
        "secretKey": TMP_SECRET_KEY,
        "createJobRequest": {
            "customId": "musicBeat",
            "inputs": [
              {
                "source": {
                    "contentId": TMP_CONTENT_ID,
                    "path": "file_path_to_root"
                }
              }
            ],
            "outputs": [
                {
                    "contentId": TMP_CONTENT_ID,
                    "destination": "/output/musicBeat",
                    "inputSelectors": [0],
                    "smartContentDescriptor": {
                        "musicBeat": {
                            "outputType": 4,
                            "drumType": 2,
                            "enlargeLevel": 3,
                            "splitNum": 3,
                        },
                    },
                }
            ],
        },
    }
    resp = requests.post(
        url=URL,
        headers=headers,
        json=request,
    )
    print(resp.text)