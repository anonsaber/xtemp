# XTemp File Hub

A simple, fast, and modern temporary file sharing service. Upload, download, copy link, and delete files easily.

## Features

- Drag & drop or click to upload files
- Download, copy link, or delete your file after upload
- Command line (curl) upload supported
- All file types supported (max size configurable)

## Usage

### Web Interface

1. Open the website in your browser.
2. Read and accept the terms by typing `ACCEPT`.
3. Drag & drop a file or click the upload area to select a file.
4. Click **Start Upload**.
5. After upload, use the **Download**, **Copy Link**, or **Delete File** buttons as needed.
6. Click **Return to Home** to upload another file.

### Command Line Example

You can upload files using `curl`:

```sh
# Method 1: Simple PUT
curl -T example.txt http://your-server.com

# Method 2: Multipart POST
curl -X POST -F "file=@example.txt" http://your-server.com/
```

After upload, you will receive a download link in the response.

To delete a file (replace `<file_url>` with your actual file link):

```sh
curl -X DELETE <file_url>
```

## How to Run

> **Recommendation:** For secure HTTPS access, it is highly recommended to deploy your own Nginx or another reverse proxy service in front of this application to handle TLS termination and SSL certificate management. The reverse proxy should forward external HTTPS traffic to the application. This setup improves security, compatibility, and allows you to manage certificates easily.

### 1. Local Storage (default, easy to start)

```sh
docker run -d -p 5000:5000 \
  -e MAX_UPLOAD_SIZE=524288000 \
  -e STORAGE_TYPE=local \
  -e XTEMP_STORAGE_PATH=/tmp \
  -e XTEMP_CONFIG_API_PASSWORD=your-strong-password \
  --name xtemp-app \
  evanshawn/xtemp:1.1
```

### 2. Cloudflare R2 Storage (recommended for production)

```sh
docker run -d -p 5000:5000 \
  -e MAX_UPLOAD_SIZE=524288000 \
  -e STORAGE_TYPE=r2 \
  -e R2_ACCOUNT_ID=your_account_id \
  -e R2_ACCESS_KEY_ID=your_access_key_id \
  -e R2_SECRET_ACCESS_KEY=your_secret_access_key \
  -e R2_BUCKET_NAME=your_backet_name \
  -e XTEMP_CONFIG_API_PASSWORD=your-strong-password \
  --name xtemp-app \
  evanshawn/xtemp:1.1
```

> ⚠️ Please replace `your_account_id`, `your_access_key_id`, `your_secret_access_key`, `your_backet_name`, and `your-strong-password` with your actual Cloudflare R2 information and a strong password.

## Dynamically Change Max Upload Size at Runtime

You can change the maximum allowed upload size without restarting the service by calling the following API:

1. **Call the API to update max upload size**  
   Use a GET request with the correct password and new size (in bytes):
   ```sh
   curl "http://your-server.com/config/set_max_upload_size?password=your-strong-password&size=104857600"
   ```
   This will set the max upload size to 100MB.

2. **Note:**  
   - The API is only available if `XTEMP_CONFIG_API_PASSWORD` is set.
   - Always use a strong password for this environment variable.

## File Expiration

The demo site at [xtemp.motofans.club](https://xtemp.motofans.club) uses Cloudflare R2's lifecycle management to automatically delete files after 24 hours.  
If you use local storage, you may need to set up a `crontab` job to periodically clean up expired files.

## License

MIT